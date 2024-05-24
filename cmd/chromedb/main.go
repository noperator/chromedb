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

	browserPath := flag.String("p", "", "path to browser profile directory")
	cookies := flag.Bool("c", false, "cookies")
	localStorage := flag.Bool("ls", false, "local storage")

	flag.Parse()

	if *cookies && *localStorage {
		fmt.Println("Error: The -c and -ls flags are mutually exclusive")
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
				value, err := chromedb.DecryptValue(c.EncryptedValue, key)
				if err != nil {
					fmt.Println("Failed to decrypt cookie %s: %v", c.Name, err)
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
			j, err := chromedb.RecordToJson(r)
			if err != nil {
				fmt.Println("Error converting record to JSON:", err)
				os.Exit(1)
			}

			fmt.Println(j)
		}
	}

}
