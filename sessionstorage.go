package chromedb

import (
	"bytes"
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

	// "time"

	"github.com/h2non/filetype"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"golang.org/x/text/encoding/unicode"
)

type SessionStorageRecord struct {
	// Value       string   `json:"value"`
	MapID       int             `json:"map_id"`
	Key         string          `json:"key"`
	Value       json.RawMessage `json:"value"`
	Charset     string          `json:"charset"`
	Decoded     string          `json:"-"`
	MIME        string          `json:"mime"`
	Conversions []string        `json:"conversions"`
	JsonType    string          `json:"json_type"`
}

type SessionStoreDb struct {
	ldb     *leveldb.DB
	Records []SessionStorageRecord `json:"records"`
}

func decodeUTF16LE(raw []byte) (string, error) {
	decoder := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder()
	utf8bytes, err := decoder.Bytes(raw)
	if err != nil {
		return "", fmt.Errorf("failed to decode UTF-16-LE: %w", err)
	}
	return string(utf8bytes), nil
}

func LoadSessionStorage(dir string) (*SessionStoreDb, error) {
	db, err := leveldb.OpenFile(dir, &opt.Options{
		ReadOnly: true,
	})

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

	ssd := &SessionStoreDb{
		ldb: db,
	}

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		value := iter.Value()

		if bytes.HasPrefix(key, []byte("map-")) {
			parts := bytes.SplitN(bytes.TrimPrefix(key, []byte("map-")), []byte("-"), 2)
			if len(parts) != 2 {
				continue
			}

			mapID, err := strconv.Atoi(string(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("failed to decode map ID: %w", err)
			}

			keyStr := string(parts[1])
			val, err := decodeUTF16LE(value)
			if err != nil {
				return nil, fmt.Errorf("failed to decode value: %w", err)
			}

			record := SessionStorageRecord{
				MapID: mapID,
				Key:   keyStr,
				// Value:   val,
				Decoded: val,
				Charset: "UTF-16-LE",
			}

			ssd.Records = append(ssd.Records, record)
		}
	}

	return ssd, nil
}

func SessionStorageRecordToJson(r SessionStorageRecord) (string, error) {
	mime := "application/octet-stream"
	xfer := []string{}
	b := []byte(r.Decoded)
	validJson := json.Valid(b)
	jsonType := ""
	out := []byte{}

	if validJson {
		// Use a custom decoder to handle large numbers as strings
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber() // This makes the decoder use json.Number for numbers
		var v interface{}
		err := d.Decode(&v)
		if err != nil {
			return "", fmt.Errorf("failed to unmarshal supposedly valid JSON: %w", err)
		}

		mime = "application/json"

		switch val := v.(type) {
		case json.Number:
			jsonType = "number"
			// Check if the number might be too large for float64
			_, err := val.Float64()
			if err != nil {
				// If it can't be converted to float64, treat it as a string in the output
				strVal := fmt.Sprintf("\"%s\"", val.String())
				out = []byte(strVal)
			} else {
				out = b
			}
		case string:
			jsonType = "string"
			out = b
		case bool:
			jsonType = "boolean"
			out = b
		case []interface{}:
			jsonType = "array"
			out = b
		case map[string]interface{}:
			jsonType = "object"
			out = b
		case nil:
			jsonType = "null"
			out = b
		default:
			jsonType = ""
			out = b
		}
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

func (ssd *SessionStoreDb) Close() {
	ssd.ldb.Close()
}
