package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
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

func getCookies(cookiesPath string) ([]Cookie, error) {

	db, err := sql.Open("sqlite3", cookiesPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

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

func getKey() ([]byte, error) {
	browserPassword := os.Getenv("BROWSER_PASSWORD")
	if browserPassword == "" {
		return []byte{}, fmt.Errorf("password not set")
	}
	password := strings.TrimSpace(string(browserPassword))
	return pbkdf2.Key([]byte(password), []byte("saltysalt"), 1003, 16, sha1.New), nil
}

func decryptValue(encryptedValue, key []byte) (string, error) {
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

	return string(decrypted[:len(decrypted)-paddingLen]), nil

}
