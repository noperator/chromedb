# ChromeDB

Read Chromium data (namely, cookies and local storage) straight from disk—_without_ spinning up the browser. <!-- Yeah, it's called <em>Chrome</em>DB, but `chromiumdb` is a bit of a mouthful for a Go package name. -->

## Description

Chromium-based browsers store cookies and local storage in the following respective databases within the profile directory:

Path | Format | Encrypted
--- | --- | ---
`Cookies` | SQLite | Yes
`Local Storage/leveldb/` | LevelDB | No

This tool reads from those databases, decrypts where necessary, and outputs the data in JSON format for easy parsing on CLI.

## Getting started

### Prerequisites

This has only been tested with Arc browser on macOS, but should work with any Chromium-based browser. I'd accept a PR to support decrypting cookies on other operating systems.

### Install

```bash
𝄢 go install -v github.com/noperator/chromedb/cmd/chromedb@latest
```

### Usage

```bash
𝄢 chromedb -h
Usage of chromedb:
  -c	cookies
  -ls
    	local storage
  -p string
    	path to browser profile directory

```

To decrypt cookies for Chromium-based Arc browser, we need to first get its password from the keychain.

```bash
𝄢  export BROWSER_PASSWORD=$(security find-generic-password -wga Arc)
𝄢 chromedb -c -p ~/Library/Application\ Support/Arc/User\ Data/Profile\ 1/ |
    shuf -n 2 |
    jq '.encrypted_value = "<ENCRYPTED>"'

{
  "domain": ".geeksforgeeks.org",
  "name": "gfg_theme",
  "encrypted_value": "<ENCRYPTED>",
  "value": "gfgThemeDark"
}
{
  "domain": "www.citrix.com",
  "name": "renderid",
  "encrypted_value": "<ENCRYPTED>",
  "value": "rend01"
}
```

Local storage is unencrypted and doesn't require a password.

```bash
𝄢 chromedb -ls -p ~/Library/Application\ Support/Arc/User\ Data/Profile\ 1/ |
    shuf -n 2 |
    jq

{
  "storage_key": "https://docs.paloaltonetworks.com",
  "script_key": "ClientSidePersistence",
  "charset": "ISO-8859-1",
  "mime": "text/plain",
  "conversions": [
    "strconv.Quote"
  ],
  "value": "ClientContext/CLIENTCONTEXT:=visitorId%3D"
}
{
  "storage_key": "https://github.com",
  "script_key": "ref-selector:greasysock/railscookie:branch",
  "charset": "ISO-8859-1",
  "mime": "application/json",
  "conversions": [],
  "value": {
    "refs": [
      "master"
    ],
    "cacheKey": "v0:1554731422.0"
  }
}
```

## Back matter

### See also

Three outstanding posts from CCL Solutions Group that helped me understand Chromium data storage:
- [LevelDB](https://www.cclsolutionsgroup.com/post/hang-on-thats-not-sqlite-chrome-electron-and-leveldb)
- [IndexedDB](https://www.cclsolutionsgroup.com/post/indexeddb-on-chromium)
- [Local storage](https://www.cclsolutionsgroup.com/post/chromium-session-storage-and-local-storage)

I almost didn't write this tool as there are many others that do this kind of thing already. The most widely used ones are written in Python ([`pycookiecheat`](https://github.com/n8henrie/pycookiecheat/blob/dev/src/pycookiecheat/chrome.py) for cookies, [`ccl_chrome_indexeddb`](https://github.com/cclgroupltd/ccl_chrome_indexeddb/blob/master/ccl_chromium_localstorage.py) for local storage)—but I avoid using Python if possible due [nightmarish environment management](https://xkcd.com/1987/). There are a few Go-based cookie-dumping utilities, but they:

- don't read from disk, and instead abuse the [remote debugging port](https://blog.chromium.org/2011/05/remote-debugging-with-chrome-developer.html) to launch a browser and dump unencrypted cookies ([`WhiteChocolateMacademiaNut`](https://github.com/slyd0g/WhiteChocolateMacademiaNut), [`chromecookiestealer`](https://github.com/magisterquis/chromecookiestealer), [`chrome-dump`](https://github.com/lesnuages/chrome-dump), [`go-chrome-stealer`](https://github.com/omaidf/go-chrome-stealer))
- only work on Windows ([`gookies`](https://github.com/CCob/gookies))
- are "obsolete" ([`gostealer`](https://github.com/4kord/gostealer)) or "demo-only" ([`go-stealer`](https://github.com/idfp/go-stealer))
- do read from disk, but either used too many hardcoded values or were too complex for my needs ([`go-chrome-cookies`](https://github.com/teocci/go-chrome-cookies), [`chrome-cookie`](https://github.com/muyids/chrome-cookie), [`ChromeDecryptor`](https://github.com/wat4r/ChromeDecryptor), [`chrome-cookie-cutter`](https://github.com/saranrapjs/chrome-cookie-cutter), [`chrome-cookie-decrypt`](https://github.com/kinghrothgar/chrome-cookie-decrypt), [`chrome-cookies`](https://github.com/igara/chrome-cookies), [`cookies`](https://github.com/creachadair/cookies))

I wasn't able to find _any_ Go-based tools that specifically parse local storage (though [`leveldb-cli`](https://github.com/cions/leveldb-cli) does read LevelDB, which is a good start).

<details><summary>(I <em>really did</em> try.)</summary>


```bash
# Search for tools written in Go that process Chrom(e|ium)'s cookies or local storage.
for QUERY in \
	'chrome cookie' \
	'chromium cookie' \
	'chrome leveldb' \
	'chromium leveldb' \
	'chrome local storage' \
	'chromium local storage' \
	; do
	echo "query: $QUERY" >&2
	curl -sv "https://github.com/search?type=repositories" \
		-G -X GET --data-urlencode "q=$QUERY language:Go" \
		-H 'accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7' |
		tee >/dev/null \
			>(htmlq -t .search-title | nl -nln | sed -E 's/(^[0-9]+)/\1.1/') \
			>(htmlq 'li > a' -a 'aria-label' | nl -nln | sed -E 's/(^[0-9]+)/\1.2/') |
	sort -V  |
	sed -E 's/^[0-9\.]+\s+//' |
	paste -d @ - - |
	tee /dev/stderr
	sleep 2
done |
    sed -E 's/ stars$//' |
	sort -u |
    tee repos.lst

# Download all of those tools' repos.
mkdir -p repos
cd repos
cat ../repos.lst | parallel --colsep @ --bar 'git clone --depth 1 https://github.com/{1} $(echo {1} | sed -E "s|/|--|")--{2}'
rm -rf ./*/.git

# Look for various topics that may or may not indicate that the tool uses the approach I care about.
echo "
sql sql|db|database|query|row
remote ws://|remote|debugg(ing|er)|9222|cdp|dev.?tools|debug.port
keychain log.?in|keychain
crypto key.?length|iteration|aes|cbc|sha1?|pbkdf|cipher|crypt
localStorage ldb|leveldb|local.?storage
" | grep -v '^$' | while read TOPIC REGEX; do
    rg -g '!.git*' -g '!topics.jsonl' -c "$REGEX" . | while read RESULT; do
        FILE=$(echo "$RESULT" | cut -d : -f 1 | sed -E 's|\./||')
        REPO="https://github.com/"$(echo "$FILE" | grep -oE '[^/]+' | head -n 1 | sed -E 's|--|/|; s/--.*//')
        STARS=$(echo "$FILE" | grep -oE '\--[0-9]+/' | head -n 1 | grep -oE '[0-9]+')
        COUNT=$(echo "$RESULT" | cut -d : -f 2)
        jo topic="$TOPIC" repo="$REPO" file="$FILE" count="$COUNT" stars="$STARS"
    done
done | tee topics.jsonl

# Sort relevant repos by popularity.
cat topics.jsonl | jq -sc 'group_by(.repo) | map({
        repo: .[0].repo,
        stars: .[0].stars,
        topics: map({topic, count}) |
            group_by(.topic) |
            map({topic: .[0].topic, count: map(.count) | add})
    }) | sort_by(.stars) | reverse[]'
    
{"repo":"https://github.com/slyd0g/WhiteChocolateMacademiaNut","stars":141,"topics":[{"topic":"crypto","count":5},{"topic":"remote","count":17},{"topic":"sql","count":6}]}
{"repo":"https://github.com/magisterquis/chromecookiestealer","stars":92,"topics":[{"topic":"crypto","count":6},{"topic":"keychain","count":3},{"topic":"remote","count":20},{"topic":"sql","count":15}]}
{"repo":"https://github.com/CCob/gookies","stars":48,"topics":[{"topic":"crypto","count":36},{"topic":"sql","count":14}]}
{"repo":"https://github.com/cions/leveldb-cli","stars":27,"topics":[{"topic":"crypto","count":14},{"topic":"localStorage","count":60},{"topic":"sql","count":290}]}
{"repo":"https://github.com/lesnuages/chrome-dump","stars":18,"topics":[{"topic":"crypto","count":11},{"topic":"keychain","count":1},{"topic":"remote","count":13}]}
{"repo":"https://github.com/teocci/go-chrome-cookies","stars":9,"topics":[{"topic":"crypto","count":140},{"topic":"keychain","count":42},{"topic":"sql","count":283}]}
{"repo":"https://github.com/idfp/go-stealer","stars":9,"topics":[{"topic":"crypto","count":73},{"topic":"keychain","count":32},{"topic":"sql","count":66}]}
{"repo":"https://github.com/muyids/chrome-cookie","stars":8,"topics":[{"topic":"crypto","count":22},{"topic":"keychain","count":3},{"topic":"sql","count":25}]}
{"repo":"https://github.com/wat4r/ChromeDecryptor","stars":5,"topics":[{"topic":"crypto","count":107},{"topic":"keychain","count":9},{"topic":"sql","count":35}]}
{"repo":"https://github.com/saranrapjs/chrome-cookie-cutter","stars":5,"topics":[{"topic":"crypto","count":29},{"topic":"sql","count":10}]}
{"repo":"https://github.com/omaidf/go-chrome-stealer","stars":5,"topics":[{"topic":"remote","count":3}]}
{"repo":"https://github.com/kawakatz/macCookies","stars":3,"topics":[{"topic":"crypto","count":68},{"topic":"keychain","count":4},{"topic":"sql","count":30}]}
{"repo":"https://github.com/4kord/gostealer","stars":3,"topics":[{"topic":"crypto","count":52},{"topic":"keychain","count":13},{"topic":"sql","count":242}]}
{"repo":"https://github.com/kinghrothgar/chrome-cookie-decrypt","stars":1,"topics":[{"topic":"crypto","count":45},{"topic":"keychain","count":12},{"topic":"sql","count":55}]}
{"repo":"https://github.com/kalelc/go-rails-cook","stars":1,"topics":[{"topic":"crypto","count":25},{"topic":"sql","count":4}]}
{"repo":"https://github.com/hybridtheory/samesite-cookie-support","stars":1,"topics":[{"topic":"crypto","count":2},{"topic":"sql","count":24}]}
{"repo":"https://github.com/m4tt72/rails-cookie-decrypt-go","stars":0,"topics":[{"topic":"crypto","count":31},{"topic":"sql","count":1}]}
{"repo":"https://github.com/igara/chrome-cookies","stars":0,"topics":[{"topic":"crypto","count":31},{"topic":"sql","count":12}]}
{"repo":"https://github.com/greasysock/railscookie","stars":0,"topics":[{"topic":"crypto","count":13},{"topic":"sql","count":1}]}
{"repo":"https://github.com/corenting/cookies","stars":0,"topics":[{"topic":"crypto","count":24},{"topic":"remote","count":1},{"topic":"sql","count":10}]}
```

</details>

### To-do

- [ ] Decrypt cookies on Linux, Windows
- [ ] Specify a domain to filter on
- [ ] Clean up error handling, logging
