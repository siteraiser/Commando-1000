# simple-gnomon-lite

An SQLITE implementation of the GNOMON smart contract indexer for DERO.

Launch Options: 

Cli-flag Port Example (auto-launches api when set):
```bash
--port=8080
```

Cli-flag Network Example:
```bash
--mode=testnet
```

Cli-flag Use Simulator Example:
```bash
--simulator
```


Configuration Options: <br>
**Memory to Use** - The amount of memory Gnomon will use before switching to disk mode. <br>
**Smoothing** - Spaces out requests using average response times. Use if default 0 is causing many timed-out requests. <br>
**Spam Level** - The amount of names a wallet can register before being considered spam. Best left at 0 if you don't need the name registrations.<br>
**Reclassify** - Re-tag and classify the SCs. This should be done after updating the search filters.<br>

Gnomon Api Method Examples:

**GetLastIndexHeight** Returns the last stored index height<br>
Request:
```bash
curl -X GET "http://localhost:8080/GetLastIndexHeight" \
```

**GetAllOwnersAndSCIDs** Returns a list of all scids and their owners <br>
Request:
```bash
curl -X GET "http://localhost:8080/GetAllOwnersAndSCIDs" \
```

**GetInitialSCIDCode** Returns the originally installed contract code <br>
Request:
```bash
curl -X GET "http://localhost:8080/GetInitialSCIDCode?scid=b77b1f5eeff6ed39c8b979c2aeb1c800081fc2ae8f570ad254bedf47bfa977f0" \
```
Response:
```json
"string..."
```
**GetAllSCIDVariableDetails**  Returns a list of all variable details by scid <br>
Request:
```bash
curl -X GET "http://localhost:8080/GetAllSCIDVariableDetails?scid=b77b1f5eeff6ed39c8b979c2aeb1c800081fc2ae8f570ad254bedf47bfa977f0" \
```
Response:
```json
[
  {
    "Key": "C",
    "Value": "/* Private Token Smart Contract Example in DVM-BASIC..."
  },
  {
    "Key": "owner",
    "Value": "dero1qy8xa5g38wmtqszw36hjc5xy63jr8a5uzfercc7cuqjp8p785p09yqgw4w5wn"
  }
]
```

**GetSCIDVariableDetailsAtTopoheight** Returns a list of all variable details by scid and at height<br>
Request:
```bash
curl -X GET "http://localhost:8080/GetSCIDVariableDetailsAtTopoheight?scid=805ade9294d01a8c9892c73dc7ddba012eaa0d917348f9b317b706131c82a2d5&height=50000" \
```
Response:
```json
[
  {
    "Key": "C",
    "Value": "Function InitializePrivate() Uint64\r\n    10 STORE(\"owner\", SIGNER())\r\n    20 RETURN 0\r\nEnd Function\r\n\r\nFunction InputStr(input String, varname String) Uint64\r\n    10 STORE(varname, input)\r\n    20 RETURN 0\r\nEnd Function"
  },
  {
    "Key": "owner",
    "Value": "dero1qytygwq00ppnef59l6r5g96yhcvgpzx0ftc0ctefs5td43vkn0p72qqlqrn8z"
  }
]
```

**GetSCIDInteractionHeight** Coming soon / subject to change <br>
Request:
```bash
curl -X GET "http://localhost:8080/GetSCIDInteractionHeight?scid=eae3f9c6f7fc1e24d17e2ce213be1ce5bbea0454f89452b09a495db43b21dcc0" \
```
Response:
```json
[700619] 
```


**GetSCIDValuesByKey** <br>
Request:
```bash
curl -X GET "http://localhost:8080/GetSCIDValuesByKey?scid=bb6e2f7dc7e09dfc42e9f357a66110e85a06c178b0018b38db57a317cbec9cdb&key=nameHdr&rmax=0" \
```
Response:
```json
{"valuesstring":["index.html"],"valuesuint64":null}
```

**GetSCIDKeysByValue** <br>
Request:
```bash
curl -X GET "// http://localhost:8080/GetSCIDKeysByValue?scid=bb6e2f7dc7e09dfc42e9f357a66110e85a06c178b0018b38db57a317cbec9cdb&val=index.html&rmax=0" \
```
Response:
```json
{"keysstring":null,"keysuint64":null}
```

**GetSCIDsByClass** <br>
Request:
```bash
curl -X GET "http://localhost:8080/GetSCIDsByClass?class=tela" \
```
Response:
```json
[
  "bb6e2f7dc7e09dfc42e9f357a66110e85a06c178b0018b38db57a317cbec9cdb",
  "a6832a5a09b82dc4b1034fd726b118da1df8ca9ad33e76bee4563e3f69d1d99a",
  "f141198eb3cccd7cb3a05ea9ecd724523d8cb8cde02ceb17670cccd4ada432f4",
  "9e0d2602a7a96764e109cddf3bb60904e964e748efd23781dc31b1ccb46a0caa"
]
```

**GetSCIDsByTags** <br>
Request:
```bash
curl -X GET "http://localhost:8080/GetSCIDsByTags?tags=G45-AT&tags=G45-C" \
```
Response:
```json
[{
    "class": "g45",
    "owner": "dero1qyx9748k9wrt89a6rm0zzlayxgs3ndkmvg6m20shqp8ynh54zf2rgqq8yn9hn",
    "scid": "abb5e45c525ac5a2f7a234f441d5ee314dc5255050557b4310aff03d1d11f3ee",
    "scname": "",
    "tags": "G45-AT"
  },
  {
    "class": "g45",
    "owner": "dero1qyx9748k9wrt89a6rm0zzlayxgs3ndkmvg6m20shqp8ynh54zf2rgqq8yn9hn",
    "scid": "f0670c0c3ff1a8d33d5f735e2549ff6d77524e57d5a21a1998d160a2f291e6f3",
    "scname": "",
    "tags": "G45-AT"
  }]
```

**Example Go App Usage** <br>
```go
package main

import (
	"bufio"
	"fmt"
	"gnomon"
	"gnomon/daemon"
	"gnomon/show"
	"gnomon/structs"
	"os"
	"strings"
)

//var Sqlite *sql.SqlStore

func main() {
	//Creat
	GConfig := gnomon.Configuration{
		RamSizeMB:   2,
		SpamLevel:   "0",
		Smoothing:   0,
		DisplayMode: -1, // Enables event channel
		Filters: map[string]map[string][]string{
			"g45": {
				"tags":    {"G45-AT", "G45-C", "G45-FAT", "G45-NAME", "T345"},
				"options": {"i"}, //regex filters for word boundry and c.i. matching (supports: "b", "i")
			},
			"nfa":   {"tags": {"ART-NFA-MS1"}},
			"swaps": {"tags": {"StartSwap"}},
			"tela":  {"tags": {"docVersion", "telaVersion"}},
		},
		//Endpoints   []daemon.Connection
		//Port        string
		CmdFlags: map[string]any{ //Override the cmd flags with desired usage
			"port":      "0",       //use 0 for none
			"mode":      "mainnet", //"testnet"
			"simulator": false,
		},
	}

	// Start Gnomon
	go func() {
		gnomon.Start(GConfig, []daemon.Connection{})
	}()

	// Create a channel to receive key events
	KeyEvents = make(chan string)
	//Start listening
	go input()
	// Catch the messges and inputs
	for {
		select {
		case kinput := <-KeyEvents:
			handleKeyInput(kinput)
		case gmsg := <-show.Events:
			fmt.Println("Gnomon", gmsg)
		}
	}
}

// Create a channel for keyboard inputs
var KeyEvents chan string

func handleKeyInput(input string) {
	sep := " "
	command, value, _ := strings.Cut(input, sep)
	if command == "pause" {
		fmt.Println("Pausing Gnomon:")
		daemon.Pause()
	} else if command == "unpause" {
		fmt.Println("Un-Pausing Gnomon:")
		daemon.UnPause()
	} else if command == "help" {
		fmt.Println(`
			Command list:
			"pause" - Stop Gnomon from indexing more blocks.
			"unpause" - Resume Gnomon indexing.
			"show tela-indexes" - Lists Tela Indexes and details.
		`)
	} else if command == "show" && value == "tela-indexes" {
		fmt.Println("Showing Tela Indexes:")
		getTelaIndexes()
	}
}

func input() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(`Type help for a list of commands`)
	text, err := reader.ReadString('\n')
	if err != nil {
		println("Error reading input:", err)
	}
	text = strings.TrimSpace(text)
	reader.Reset(os.Stdin)
	go input()
	KeyEvents <- text
}

func getTelaIndexes() {

	scids := gnomon.Sqlite.GetSCIDsByTags([]string{"telaVersion"})

	for _, scid := range scids {
		fmt.Println("")
		fmt.Println("Tela Index SCID:", scid)
		var hVars []*structs.SCIDVariable
		hVars = gnomon.Sqlite.GetSCIDVariableDetailsAtTopoheight(scid, int64(5500000))

		for _, variable := range hVars {
			if variable.Key == "nameHdr" ||
				variable.Key == "iconURLHdr" ||
				variable.Key == "descrHdr" {
				fmt.Println(variable.Key, ":", variable.Value)
			}
		}
	}
}

/* Another way to access the db
db_name := fmt.Sprintf("sql%s.db", "GNOMON")
db_path := filepath.Join(GConfig.CmdFlags["mode"].(string), "gnomondb")
Sqlite, _ = sql.NewDiskDB(db_path, db_name)
*/
```