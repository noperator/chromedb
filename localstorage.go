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
	"time"

	"github.com/h2non/filetype"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"golang.org/x/text/encoding/unicode"
	"google.golang.org/protobuf/encoding/protowire"
)

func fromChromeTimestamp(microseconds int64) (time.Time, error) {
	chromiumEpoch := time.Date(1601, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro()
	microFromEpoch := chromiumEpoch + microseconds
	timestamp := time.Unix(0, microFromEpoch*1000)
	return timestamp, nil
}

func decodeString(raw []byte) (string, string, error) {
	prefix := raw[0]
	if prefix == 0 {
		decoder := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder()
		utf8bytes, err := decoder.Bytes(raw[1:])
		if err != nil {
			return "", "", fmt.Errorf("failed to decode UTF-16-LE: %w", err)
		}
		return string(utf8bytes), "UTF-16-LE", nil
	} else if prefix == 1 {
		return string(raw[1:]), "ISO-8859-1", nil
	}
	return "", "", fmt.Errorf("unknown string encoding prefix: %d", prefix)
}

type StorageMetadata struct {
	StorageKey string    `json:"storage_key"`
	Timestamp  time.Time `json:"timestamp"`
	Size       int       `json:"size"`
}

type LocalStorageRecord struct {
	StorageKey  string          `json:"storage_key"`
	ScriptKey   string          `json:"script_key"`
	Charset     string          `json:"charset"`
	Decoded     string          `json:"-"`
	MIME        string          `json:"mime"`
	Conversions []string        `json:"conversions"`
	JsonType    string          `json:"-"`
	Value       json.RawMessage `json:"value"`
}

type LocalStoreDb struct {
	ldb      *leveldb.DB
	Records  []LocalStorageRecord `json:"records"`
	metadata []StorageMetadata    `json:"metadata"`
}

func StorageMetadataFromProtobuff(sm *StorageMetadata, data []byte) error {

	fieldNum, wireType, n := protowire.ConsumeTag(data)
	if fieldNum != 1 || wireType != protowire.VarintType {
		return fmt.Errorf("Expected field number 1 with varint type, got field number %d with wire type %d", fieldNum, wireType)
	}
	timestamp, m := protowire.ConsumeVarint(data[n:])
	if m < 0 {
		return fmt.Errorf("Failed to decode timestamp")
	}

	fieldNum, wireType, n = protowire.ConsumeTag(data[n+m:])
	if fieldNum != 2 || wireType != protowire.VarintType {
		return fmt.Errorf("Expected field number 2 with varint type, got field number %d with wire type %d", fieldNum, wireType)
	}
	size, m := protowire.ConsumeVarint(data[n+m:])
	if m < 0 {
		return fmt.Errorf("Failed to decode size")
	}

	ts, err := fromChromeTimestamp(int64(timestamp))
	if err != nil {
		return fmt.Errorf("Failed to decode timestamp: %w", err)
	}

	sm.Timestamp = ts
	sm.Size = int(size)

	return nil
}

func LoadLocalStorage(dir string) (*LocalStoreDb, error) {
	db := &leveldb.DB{}
	db, err := leveldb.OpenFile(dir, &opt.Options{
		ReadOnly: true,
	})

	// We try the ReadOnly option above, but it weirdly doesn't work when the
	// db is locked. When this happens, we simply copy the db to memory and
	// read from there.
	if err != nil {

		srcDir := dir

		memStorage := storage.NewMemStorage()

		// Copy the LevelDB directory contents into the memory storage
		err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {

			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(srcDir, path)
			if err != nil {
				return err
			}

			// Skip directories, we only need files
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

			// Determine the file descriptor type
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

			// Create the file in the memory storage
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

			// Write the contents to the memory storage
			_, err = writer.Write(data)
			if err != nil {
				writer.Close()
				return err
			}

			// Close the writer
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

		// Open the LevelDB using the memory storage
		db, err = leveldb.Open(memStorage, nil)
		if err != nil {
			fmt.Println("Error opening LevelDB:", err)
			return nil, err
		}

	}
	defer db.Close()

	lsd := &LocalStoreDb{
		ldb: db,
	}

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		value := iter.Value()

		const (
			MetaKeyPrefix   = "META:"
			RecordKeyPrefix = "_"
		)

		// metadata
		if bytes.HasPrefix(key, []byte(MetaKeyPrefix)) {
			storageKey := string(bytes.TrimPrefix(key, []byte(MetaKeyPrefix)))

			metadata := StorageMetadata{
				StorageKey: storageKey,
				Timestamp:  time.Time{},
				Size:       0,
			}
			err := StorageMetadataFromProtobuff(&metadata, value)
			if err != nil {
				return nil, err
			}

			lsd.metadata = append(lsd.metadata, metadata)

			// record
		} else if bytes.HasPrefix(key, []byte(RecordKeyPrefix)) {
			parts := bytes.SplitN(bytes.TrimPrefix(key, []byte(RecordKeyPrefix)), []byte{0}, 2)
			if len(parts) != 2 {
				continue
			}

			record := LocalStorageRecord{}

			storageKey := string(parts[0])
			record.StorageKey = storageKey
			sk, _, err := decodeString(parts[1])
			if err != nil {
				return nil, fmt.Errorf("failed to decode script key: %w", err)
			}
			record.ScriptKey = sk

			val, valEnc, err := decodeString(value)
			if err != nil {
				return nil, fmt.Errorf("failed to decode value: %w", err)
			}
			record.Decoded = val
			record.Charset = valEnc

			lsd.Records = append(lsd.Records, record)
		}
	}

	return lsd, nil
}

func LocalStorageRecordToJson(r LocalStorageRecord) (string, error) {

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

		// Determine the type of the JSON value
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

func (lsd *LocalStoreDb) Close() {
	lsd.ldb.Close()
}
