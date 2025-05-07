package chromedb

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/pbkdf2"
)

type Cookie struct {
	Domain         string `json:"domain"`
	Name           string `json:"name"`
	EncryptedValue []byte `json:"encrypted_value"`
	Value          string `json:"value"`
}

func GetCookies(cookiesPath string) ([]Cookie, error) {

	db, err := sql.Open("sqlite3", cookiesPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Check the database version - we'll use this later for decryption
	var dbVersion int
	row := db.QueryRow("SELECT value FROM meta WHERE key = 'version'")
	if err := row.Scan(&dbVersion); err != nil {
		// If we can't get the version, assume it's an older version
		dbVersion = 0
	}
	
	// Store the database version in a package variable for DecryptValue to use
	currentDBVersion = dbVersion

	// query := "SELECT name, value, host_key, encrypted_value FROM cookies WHERE host_key like ?"
	// rows, err := db.Query(query, fmt.Sprintf("%%%s%%", domain))
	query := "SELECT name, value, host_key, encrypted_value FROM cookies"
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cookies []Cookie
	for rows.Next() {
		var cookie Cookie
		err := rows.Scan(&cookie.Name, &cookie.Value, &cookie.Domain, &cookie.EncryptedValue)
		if err != nil {
			return nil, err
		}
		cookies = append(cookies, cookie)
	}

	return cookies, nil
}

func GetKey() ([]byte, error) {
	browserPassword := os.Getenv("BROWSER_PASSWORD")
	if browserPassword == "" {
		return []byte{}, fmt.Errorf("BROWSER_PASSWORD environment variable not set")
	}
	password := strings.TrimSpace(string(browserPassword))
	return pbkdf2.Key([]byte(password), []byte("saltysalt"), 1003, 16, sha1.New), nil
}

// Package variable to store the current database version
var currentDBVersion int

func DecryptValue(encryptedValue, key []byte, domain string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	// The EncryptedValue is prefixed with "v10", remove it
	// TODO check if prefix is v10
	if len(encryptedValue) < 3 {
		return "", fmt.Errorf("encrypted length less than 3")
	}
	version := string(encryptedValue[0:3])
	if version != "v10" {
		return "", fmt.Errorf("unsported encrypted value version: %s", version)
	}
	encryptedValue = encryptedValue[3:]
	decrypted := make([]byte, len(encryptedValue))
	const (
		aescbcSalt            = `saltysalt`
		aescbcIV              = `                `
		aescbcIterationsLinux = 1
		aescbcIterationsMacOS = 1003
		aescbcLength          = 16
	)
	cbc := cipher.NewCBCDecrypter(block, []byte(aescbcIV))
	cbc.CryptBlocks(decrypted, encryptedValue)

	if len(decrypted) == 0 {
		return "", fmt.Errorf("not enough bits")
	}

	if len(decrypted)%aescbcLength != 0 {
		return "", fmt.Errorf("decrypted data block length is not a multiple of %d", aescbcLength)
	}
	paddingLen := int(decrypted[len(decrypted)-1])
	if paddingLen > 16 {
		return "", fmt.Errorf("invalid last block padding length: %d", paddingLen)
	}

	// In Chrome database versions ≥ 24, the first 32 bytes contain a SHA256 digest of the host_key (domain)
	// This was added in Chrome v130 (https://github.com/chromium/chromium/commit/5ea6d65c622a3d5ff75db9dc0257ea3869f31289)
	if currentDBVersion >= 24 {
		// Need to verify and skip the first 32 bytes (SHA256 digest of domain)
		if len(decrypted) <= 32 {
			return "", fmt.Errorf("decrypted data too short for db version %d, expected more than 32 bytes but got %d", currentDBVersion, len(decrypted))
		}
		
		// If domain is provided, verify the SHA256 hash matches
		if domain != "" {
			// Calculate SHA256 hash of the domain
			domainHash := sha256.Sum256([]byte(domain))
			
			// Check if the hash in the decrypted value matches the calculated hash
			if !bytes.Equal(domainHash[:], decrypted[:32]) { // SHA256 is 32 bytes
				return "", fmt.Errorf("domain hash verification failed")
			}
		}
		
		return string(decrypted[32:len(decrypted)-paddingLen]), nil
	}
	
	// For older versions, return the full decrypted value (minus padding)
	return string(decrypted[:len(decrypted)-paddingLen]), nil
}
