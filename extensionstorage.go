package chromedb

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"unicode/utf8"

	"github.com/h2non/filetype"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

type ExtensionStorageRecord struct {
	ExtensionID string          `json:"extension_id"`
	Key         string          `json:"key"`
	Charset     string          `json:"charset"`
	Decoded     string          `json:"-"`
	MIME        string          `json:"mime"`
	Conversions []string        `json:"conversions"`
	JsonType    string          `json:"-"`
	Value       json.RawMessage `json:"value"`
}

type ExtensionStoreDb struct {
	ldb     *leveldb.DB
	Records []ExtensionStorageRecord `json:"records"`
}

func LoadExtensionStorage(dir string) (*ExtensionStoreDb, error) {
	db := &leveldb.DB{}
	db, err := leveldb.OpenFile(dir, &opt.Options{
		ReadOnly: true,
	})

	// Handle locked DB by copying to memory
	if err != nil {
		srcDir := dir
		memStorage := storage.NewMemStorage()

		err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(srcDir, path)
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			srcFile, err := os.Open(path)
			if err != nil {
				return err
			}
			defer srcFile.Close()

			data, err := io.ReadAll(srcFile)
			if err != nil {
				return err
			}

			var num int64
			num = 0
			re := regexp.MustCompile(`\d+`)
			match := re.FindString(relPath)
			if match != "" {
				matchInt, err := strconv.Atoi(match)
				if err == nil {
					num = int64(matchInt)
				}
			}

			var fileType storage.FileType
			switch {
			case strings.HasSuffix(relPath, ".ldb"):
				fileType = storage.TypeTable
			case strings.HasPrefix(relPath, "MANIFEST"):
				fileType = storage.TypeManifest
			case strings.HasSuffix(relPath, ".log"):
				fileType = storage.TypeJournal
			case strings.HasSuffix(relPath, ".tmp"):
				fileType = storage.TypeTemp
			default:
				return nil
			}

			fd := storage.FileDesc{Type: fileType, Num: num}
			if fd.Type == storage.TypeManifest {
				err = memStorage.SetMeta(fd)
				if err != nil {
					return err
				}
			}
			writer, err := memStorage.Create(fd)
			if err != nil {
				return err
			}

			_, err = writer.Write(data)
			if err != nil {
				writer.Close()
				return err
			}

			err = writer.Close()
			if err != nil {
				return err
			}

			return nil
		})

		if err != nil {
			fmt.Println("Error copying directory:", err)
			return nil, err
		}

		db, err = leveldb.Open(memStorage, nil)
		if err != nil {
			fmt.Println("Error opening LevelDB:", err)
			return nil, err
		}
	}
	defer db.Close()

	esd := &ExtensionStoreDb{
		ldb: db,
	}

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		value := iter.Value()

		record := ExtensionStorageRecord{}

		// Extension settings are stored directly with the key as the setting name
		record.Key = string(key)
		record.ExtensionID = filepath.Base(dir)

		// Try UTF-8 first since it's more common for extension settings
		record.Decoded = string(value)
		record.Charset = "UTF-8"

		// Only try UTF-16 if the UTF-8 string contains invalid characters
		if !utf8.ValidString(record.Decoded) {
			if val, err := decodeUTF16LE(value); err == nil {
				record.Decoded = val
				record.Charset = "UTF-16-LE"
			}
		}

		esd.Records = append(esd.Records, record)
	}

	return esd, nil
}

func ExtensionStorageRecordToJson(r ExtensionStorageRecord) (string, error) {
	mime := "application/octet-stream"
	xfer := []string{}
	b := []byte(r.Decoded)
	validJson := json.Valid(b)
	jsonType := ""
	out := []byte{}

	if validJson {
		var v interface{}
		err := json.Unmarshal(b, &v)
		if err != nil {
			return "", fmt.Errorf("failed to unmarshal supposedly valid JSON: %w", err)
		}

		mime = "application/json"

		switch v.(type) {
		case float64:
			jsonType = "number"
		case string:
			jsonType = "string"
		case bool:
			jsonType = "boolean"
		case []interface{}:
			jsonType = "array"
		case map[string]interface{}:
			jsonType = "object"
		case nil:
			jsonType = "null"
		default:
			jsonType = ""
		}

		out = b
	} else {
		quoted := strconv.Quote(r.Decoded)
		if json.Valid([]byte(quoted)) {
			out = []byte(quoted)
			mime = "text/plain"
			xfer = append(xfer, "strconv.Quote")
			mime = http.DetectContentType(b)
			mime = strings.Split(mime, ";")[0]
		} else {
			b64 := base64.StdEncoding.EncodeToString(b)
			xfer = append(xfer, "base64.StdEncoding.EncodeToString")
			out = []byte(strconv.Quote(b64))
			xfer = append(xfer, "strconv.Quote")

			magic, _ := filetype.Match(b)
			if magic != filetype.Unknown {
				mime = magic.MIME.Value
			}
		}
	}

	r.MIME = mime
	r.Conversions = xfer
	r.Value = out
	r.JsonType = jsonType
	recordJson, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("failed to marshal record to JSON: %w", err)
	}

	return string(recordJson), nil
}

func (esd *ExtensionStoreDb) Close() {
	esd.ldb.Close()
}
