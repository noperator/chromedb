package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/noperator/chromedb"
)

func main() {

	browserPath := flag.String("p", "", "path to browser profile directory (required)")
	cookies := flag.Bool("c", false, "cookies")
	localStorage := flag.Bool("ls", false, "local storage")
	sessionStorage := flag.Bool("ss", false, "session storage")

	flag.Parse()

	if *browserPath == "" {
		fmt.Println("Error: -p flag (path to browser profile directory) is required")
		flag.Usage()
		os.Exit(1)
	}

	// Check for mutually exclusive flags.
	flagCount := 0
	if *cookies {
		flagCount++
	}
	if *localStorage {
		flagCount++
	}
	if *sessionStorage {
		flagCount++
	}

	if flagCount != 1 {
		fmt.Println("Error: Please specify exactly one of -c, -ls, or -ss")
		flag.Usage()
		os.Exit(1)
	}

	if *cookies {

		cookiesPath := filepath.Join(*browserPath, "Cookies")
		cookies, err := chromedb.GetCookies(cookiesPath)
		if err != nil {
			fmt.Println("Error opening Cookies database:", err)
			os.Exit(1)
		}

		key, err := chromedb.GetKey()
		if err != nil {
			fmt.Println("Error getting key:", err)
			os.Exit(1)
		}

		for _, c := range cookies {
			if len(c.EncryptedValue) > 0 {
				value, err := chromedb.DecryptValue(c.EncryptedValue, key, c.Domain)
				if err != nil {
					fmt.Printf("Failed to decrypt cookie %s: %v\n", c.Name, err)
				}
				c.Value = value
			}

			j, err := json.Marshal(c)
			if err != nil {
				fmt.Println("Error converting cookie to JSON:", err)
				os.Exit(1)
			}

			fmt.Println(string(j))
		}
	}

	if *localStorage {
		localStoragePath := filepath.Join(*browserPath, "Local Storage/leveldb")

		lsd, err := chromedb.LoadLocalStorage(localStoragePath)
		if err != nil {
			fmt.Println("Error opening LevelDB:", err)
			os.Exit(1)
		}
		defer lsd.Close()

		for _, r := range lsd.Records {
			j, err := chromedb.LocalStorageRecordToJson(r)
			if err != nil {
				fmt.Println("Error converting record to JSON:", err)
				os.Exit(1)
			}

			fmt.Println(j)
		}
	}

	if *sessionStorage {
		sessionStoragePath := filepath.Join(*browserPath, "Session Storage")

		ssd, err := chromedb.LoadSessionStorage(sessionStoragePath)
		if err != nil {
			fmt.Println("Error opening LevelDB:", err)
			os.Exit(1)
		}
		defer ssd.Close()

		for _, r := range ssd.Records {
			j, err := chromedb.SessionStorageRecordToJson(r)
			if err != nil {
				fmt.Println("Error converting record to JSON:", err)
				os.Exit(1)
			}

			fmt.Println(j)
		}
	}
}
