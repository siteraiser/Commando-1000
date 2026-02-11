package main

import (
	"encoding/hex"
	"fmt"
	"gnomon"
	sql "gnomon/db"
	"strconv"
	"strings"
	"time"

	"github.com/deroproject/derohe/cryptography/crypto"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/transaction"
	"github.com/fxamacker/cbor/v2"
)

func getProof() {
	txhash := getText(`Enter TX to get proof for:`)
	if len(txhash) == 64 {
		_, err := hex.DecodeString(txhash)
		if err != nil {
			fmt.Println(err, "Error parsing txhash")
			return
		}
		key := dero.Wallet.GetTXKey(txhash)
		if key != "" {
			fmt.Println("TX Proof key:", key)
		} else {
			fmt.Println(err, "TX not found in database")
		}
	} else {
		fmt.Println("get_tx_key needs transaction hash as input parameter")
		fmt.Println("eg. get_tx_key ea551b02b9f1e8aebe4d7b1b7f6bf173d76ae614cb9a066800773fee9e226fd7")
	}
}

func listTxs() {
	if dero.Wallet == nil {
		fmt.Println("No wallet opened.")
		return
	}
	in := true
	out := true
	coinbase := true
	max_height := dero.Wallet.Get_Height()
	transfers := dero.Wallet.Show_Transfers(crypto.ZEROHASH, coinbase, in, out, 0, max_height, "", "", 0, 0)
	for _, t := range transfers {
		var args rpc.Arguments
		if t.Coinbase {
			fmt.Println(fmt.Sprintf(
				"%s Height %d TopoHeight %d  Coinbase (miner reward) received %s DERO\n",
				t.Time.Format(time.RFC822),
				t.Height,
				t.TopoHeight,
				globals.FormatMoney(t.Amount),
			))
		} else {
			if t.PayloadType == 0 {
				args, _ = t.ProcessPayload()
			} else if t.PayloadType == 1 && t.PayloadError != "" { //&& t.Status == 1
				args = processPayload(&t)
			}

			fmt.Println(fmt.Sprintf("%s Height %d TopoHeight %d transaction %s received %s DERO Proof: %s RPC CALL arguments %s "+"\n",
				t.Time.Format(time.RFC822),
				t.Height, t.TopoHeight,
				t.TXID,
				globals.FormatMoney(t.Amount),
				t.Proof,
				args,
			))
		}
	}
}

func showComments(command, value string) {
	if value == "" {
		listComments(true, true)
	} else if value == "incoming" {
		listComments(true, false)
	} else if value == "outgoing" {
		listComments(false, true)
	} else {
		fmt.Println("Error processing command (" + command + " " + value + ")")
	}
}

func listComments(in, out bool) {
	if dero.Wallet == nil {
		fmt.Println("No wallet opened.")
		return
	}
	coinbase := false
	println("Syncing...")
	dero.Wallet.Sync_Wallet_Memory_With_Daemon()
	time.Sleep(time.Second)

	// Check receiver
	entries := dero.Wallet.Show_Transfers(crypto.ZEROHASH, coinbase, in, out, uint64(0), uint64(0), "", "", 0, 0)

	for i, _ := range entries {
		entries[i].ProcessPayload()
		showPayload(&entries[i])
	}
}

var ecount = 0

func processPayload(e *rpc.Entry) (args rpc.Arguments) {
	var err error
	var dest_port, value, needs_reply_address uint64
	var comment string
	var reply_address *rpc.Address
	var v map[string]interface{}
	_, _ = cbor.UnmarshalFirst(e.Payload, &v)
	if val, exists := v["CS"]; exists {
		comment = val.(string)
	}
	if val, exists := v["DU"]; exists {
		dest_port = val.(uint64)
	}
	if val, exists := v["VU"]; exists {
		value = val.(uint64)
	}
	if val, exists := v["NS"]; exists { //in case it is here
		reply_address, err = globals.ParseValidateAddress(val.(string))
		if err != nil {
			needs_reply_address = 1
		}
	}
	if val, exists := v["NU"]; exists { //in case it is here
		needs_reply_address, _ = val.(uint64)
	}
	if val, exists := v["RA"]; exists {
		reply_address, _ = rpc.NewAddressFromCompressedKeys(val.([]uint8))
	}
	if val, exists := v["RS"]; exists {
		reply_address, _ = globals.ParseValidateAddress(val.(string))
	}

	args = rpc.Arguments{
		rpc.Argument{
			Name: "RPC_DESTINATION_PORT", DataType: "U", Value: dest_port,
		},
		rpc.Argument{
			Name: "RPC_VALUE_TRANSFER", DataType: "U", Value: value,
		},
		rpc.Argument{
			Name: "RPC_COMMENT", DataType: "S", Value: comment,
		},
		rpc.Argument{
			Name: "RPC_REPLYBACK_ADDRESS", DataType: "S", Value: reply_address,
		},

		rpc.Argument{
			Name: "RPC_NEEDS_REPLYBACK_ADDRESS", DataType: "U", Value: needs_reply_address,
		},
	}
	return
}
func showPayload(entry *rpc.Entry) {
	var args rpc.Arguments
	var to_address *rpc.Address
	var from_address *rpc.Address
	var reply_address *rpc.Address
	value := uint64(0)
	dest_port := uint64(0)
	comment := ""

	if !entry.Incoming && !entry.Coinbase { //outgoing
		to_address, _ = globals.ParseValidateAddress(entry.Destination)
	}
	if entry.PayloadType == 1 && entry.PayloadError != "" { //&& t.Status == 1
		args = processPayload(entry)
	}
	if entry.Incoming {
		args = entry.Payload_RPC
		// Should check for spoofing here...
		if entry.Payload_RPC.Has(rpc.RPC_REPLYBACK_ADDRESS, rpc.DataString) {
			reply_address, _ = globals.ParseValidateAddress(entry.Payload_RPC.Value(rpc.RPC_REPLYBACK_ADDRESS, rpc.DataString).(string))
		} else if entry.Payload_RPC.Has(rpc.RPC_REPLYBACK_ADDRESS, rpc.DataAddress) {
			reply_address, _ = globals.ParseValidateAddress(entry.Payload_RPC.Value(rpc.RPC_REPLYBACK_ADDRESS, rpc.DataAddress).(rpc.Address).String())
		}
	}

	if args.HasValue(rpc.RPC_VALUE_TRANSFER, rpc.DataUint64) {
		value = args.Value(rpc.RPC_VALUE_TRANSFER, rpc.DataUint64).(uint64)
	}
	if args.HasValue(rpc.RPC_COMMENT, rpc.DataString) {
		comment = args.Value(rpc.RPC_COMMENT, rpc.DataString).(string)
	}
	if args.HasValue(rpc.RPC_DESTINATION_PORT, rpc.DataUint64) {
		dest_port = args.Value(rpc.RPC_DESTINATION_PORT, rpc.DataUint64).(uint64)
	}

	if reply_address == nil && entry.Sender != "" && entry.Sender != dero.Wallet.GetAddress().String() {
		from_address, _ = globals.ParseValidateAddress(entry.Sender)
	}
	txt := "Coinbase"
	if entry.Incoming {
		txt = "Incoming"
	} else if !entry.Coinbase {
		txt = "Outgoing"
	}
	if comment == "" {
		return
	}
	ecount++
	fmt.Println("* " + txt + " Entry * " + strconv.Itoa(ecount) + " *******************")
	fmt.Println("Time UTC:", entry.Time.UTC().Format("2006-01-02 15:04:05"))

	if entry.Coinbase {
		// do nothing for now
	} else if entry.Incoming {
		if reply_address != nil {
			fmt.Println("From (Reply-back):", reply_address.String())
		} else if from_address != nil {
			fmt.Println("From:", from_address.String())
		}
	} else {
		if to_address != nil {
			fmt.Println("To:", to_address.String())
		}
	}
	fmt.Println("Dero Amount:", globals.FormatMoney(entry.Amount))
	if value != 0 {
		fmt.Println("Value Transfer:", value)
	}
	fmt.Println("Comment:", comment)
	fmt.Println("Destination Port:", dest_port)
	println("")
}

func getComment() (comment string) {
	for {
		text := getText("Comment")
		if len(text) <= 100 {
			return text
		}
		fmt.Println("Comment too long. ", len(text))
	}
}

// Transactions
func sendDero() {
	if dero.Wallet == nil {
		fmt.Println("No wallet opened.")
		return
	}
	var scid crypto.Hash
	// Check Dero balance
	max_balance, _, err := dero.Wallet.GetDecryptedBalanceAtTopoHeight(crypto.ZEROHASH, -1, dero.Wallet.GetAddress().String())
	if err != nil {
		fmt.Println(err, "Error getting balance for scid:", scid.String())
		return
	}
	max_str := globals.FormatMoney(max_balance)

	receiver := getText(`Enter Recipient's Dero Address:`)
	address, err := globals.ParseValidateAddress(receiver)
	if err != nil || address.String() == dero.Wallet.GetAddress().String() {
		fmt.Println("Error with recipient address. ", err)
		return
	}

	var arguments = rpc.Arguments{}
	var amount_to_transfer uint64

	if address.IsIntegratedAddress() {

		if address.Arguments.Validate_Arguments() != nil {
			fmt.Println("Invalid integrated address.")
			return
		}
		if !address.Arguments.Has(rpc.RPC_DESTINATION_PORT, rpc.DataUint64) {
			fmt.Println("Integrated address missing destination port.")
			return
		}
		// Add port
		fmt.Println("Destination port is integrated in address:", address.Arguments.Value(rpc.RPC_DESTINATION_PORT, rpc.DataUint64).(uint64))
		arguments = append(arguments, rpc.Argument{
			Name:     rpc.RPC_DESTINATION_PORT,
			DataType: rpc.DataUint64,
			Value:    uint64(address.Arguments.Value(rpc.RPC_DESTINATION_PORT, rpc.DataUint64).(uint64)),
		})
		// Add amount
		if address.Arguments.Has(rpc.RPC_VALUE_TRANSFER, rpc.DataUint64) {
			fmt.Println("Transaction send amount:", globals.FormatMoney(address.Arguments.Value(rpc.RPC_VALUE_TRANSFER, rpc.DataUint64).(uint64)))
			amount_to_transfer = address.Arguments.Value(rpc.RPC_VALUE_TRANSFER, rpc.DataUint64).(uint64)
		}
		// Check expiration status
		if address.Arguments.Has(rpc.RPC_EXPIRY, rpc.DataTime) {
			if address.Arguments.Value(rpc.RPC_EXPIRY, rpc.DataTime).(time.Time).Before(time.Now().UTC()) {
				fmt.Println("I.A. expired:", address.Arguments.Value(rpc.RPC_EXPIRY, rpc.DataTime))
				return
			} else {
				fmt.Println("I.A. expires:", address.Arguments.Value(rpc.RPC_EXPIRY, rpc.DataTime))
			}
		} else {
			arguments = append(arguments, rpc.Argument{
				Name:     rpc.RPC_EXPIRY,
				DataType: rpc.DataTime,
				Value:    time.Now().UTC(),
			})
		}
		// Add comment
		if address.Arguments.Has(rpc.RPC_COMMENT, rpc.DataString) {
			fmt.Println("Integrated Comment:", address.Arguments.Value(rpc.RPC_COMMENT, rpc.DataString))
			arguments = append(arguments, rpc.Argument{
				Name:     rpc.RPC_COMMENT,
				DataType: rpc.DataString,
				Value:    address.Arguments.Value(rpc.RPC_COMMENT, rpc.DataString),
			})
		}
		// Add address for reply back
		if address.Arguments.Has(rpc.RPC_NEEDS_REPLYBACK_ADDRESS, rpc.DataUint64) { //is has enough?
			fmt.Println("Adding your reply-back address to message.")
			arguments = append(arguments,
				rpc.Argument{Name: rpc.RPC_REPLYBACK_ADDRESS,
					DataType: rpc.DataAddress,
					Value:    dero.Wallet.GetAddress(),
				})
		}

	} else {

		// Regular send
		amount_str := getText(fmt.Sprintf("Enter Dero amount to transfer (max %s): ", max_str))
		if amount_str == "" {
			amount_str = ".00001"
		}
		amount_to_transfer, err = globals.ParseAmount(amount_str)
		if err != nil {
			fmt.Println(err, "Error parsing amount.")
			return // invalid amount provided, bail out
		}

		comment := getComment()

		if comment != "" {
			arguments = rpc.Arguments{
				{Name: rpc.RPC_VALUE_TRANSFER, DataType: rpc.DataUint64, Value: amount_to_transfer},
				{Name: rpc.RPC_COMMENT, DataType: rpc.DataString, Value: comment},
			}
			// message send?
			res := getText("Send with reply-back address? (y/n)")

			if res == "y" {
				arguments = append(arguments,
					rpc.Arguments{
						{Name: rpc.RPC_REPLYBACK_ADDRESS, DataType: rpc.DataAddress, Value: dero.Wallet.GetAddress()},
						{Name: rpc.RPC_NEEDS_REPLYBACK_ADDRESS, DataType: rpc.DataUint64, Value: 1},
					}...,
				)
			}
			dport := 0
			res = getText("Enter port if desired or enter to continue:")
			if res != "" {
				dport, _ = strconv.Atoi(res)
			}

			arguments = append(arguments, rpc.Argument{Name: rpc.RPC_DESTINATION_PORT, DataType: rpc.DataUint64, Value: uint64(dport)})
		}
	}
	// Get the ringsize
	ringsize := 0
	rstext := getText(`Enter ringsize (8 is default):`)
	if rstext != "" {
		ringsize, err = strconv.Atoi(rstext)
		if err != nil {
			fmt.Println(err, "Error parsing ringsize.")
			return
		}
	}
	// Check packing
	if _, err := arguments.CheckPack(transaction.PAYLOAD0_LIMIT); err != nil {
		fmt.Println(err, "Arguments packing error.")
		return
	}
	// Check address
	if dero.Wallet.GetAddress().String() == address.BaseAddress().String() {
		fmt.Println("Can't send to self. TX send Cancelled.")
		return
	}
	// Last chance to cancel
	pass := getText(`Enter password to send:`)
	if !checkPass(pass) {
		fmt.Println("Incorrect Password.")
		return
	}
	// Send one TX with payload
	fmt.Println("Building TX...")
	tx, err := dero.Wallet.TransferPayload0([]rpc.Transfer{{SCID: scid, Amount: amount_to_transfer, Destination: address.String(), Payload_RPC: arguments}}, uint64(ringsize), false, rpc.Arguments{}, 0, false) // empty SCDATA((uint64(dero.Wallet.GetRingSize())+1)*config.FEE_PER_KB)/4
	if err != nil {
		fmt.Println(err, "Error building transaction.")
		return
	}
	fmt.Println("Sending TX...")
	if err = dero.Wallet.SendTransaction(tx); err != nil {
		fmt.Println(err, "Error sending transaction.")
		return
	}
	fmt.Println("Dispatched TX with txid:", tx.GetHash().String())
}

func sendToken(input string) {
	var scid crypto.Hash
	if input == "" {
		return

	}
	input = strings.TrimSpace(getText(`Enter Token SCID:`))
	scid = crypto.HexToHash(input)

	token_name := "token"
	//if gnomon.TargetHeight > 0
	hVars := gnomon.Sqlite.GetAllSCIDVariableDetails(scid.String())
	for _, v := range hVars {
		switch v.Key.(type) {
		case string:
			if strings.Contains(v.Key.(string), "nameHdr") {
				token_name = v.Key.(string)
			}
		}
	}

	if dero.Wallet == nil {
		fmt.Println("No wallet opened.")
		return
	}

	if !checkPass(getPassword(`Enter Password:`)) {
		fmt.Println("Incorrect Password")
		return
	}

	var amount_to_transfer uint64
	max_balance, _, err := dero.Wallet.GetDecryptedBalanceAtTopoHeight(scid, -1, dero.Wallet.GetAddress().String())
	if err != nil {
		fmt.Println(err, "error during SC balance", "scid", scid.String())
		return
	}

	if err := dero.Wallet.TokenAdd(scid); err != nil {
		fmt.Println(err, "Error adding SCID:", scid.String())
		return
	}
	dero.Wallet.GetAccount().Balance[scid] = max_balance

	fmt.Println("Your "+token_name+" balance:", max_balance)

	receiver := getText(`Enter Token Recipient's Address:`)

	address, err := globals.ParseValidateAddress(receiver)
	if err != nil || address.String() == dero.Wallet.GetAddress().String() {
		fmt.Println("Error with recipient address ", err)
		return
	}

	max_str := globals.FormatMoney(max_balance)
	amount_str := getText(fmt.Sprintf("Enter "+token_name+" amount to transfer (max %s): ", max_str))

	if amount_str == "" {
		amount_str = ".00001"
	}
	amount_to_transfer, err = globals.ParseAmount(amount_str)
	if err != nil || amount_to_transfer == 0 {
		fmt.Println(err, "Err parsing amount")
		return // invalid amount provided, bail out
	}

	ringsize := 0
	/*
		rstext := getText(`Enter Ringsize (8 is default):`)
		if rstext != "" {
			ringsize, err = strconv.Atoi(rstext)
			if err != nil {
				fmt.Println(err, "Err parsing ringsize")
				return
			}
		}
	*/
	tx, err := dero.Wallet.TransferPayload0([]rpc.Transfer{{SCID: scid, Amount: amount_to_transfer, Destination: address.String()}}, uint64(ringsize), false, rpc.Arguments{}, 0, false) // empty SCDATA

	if err != nil {
		fmt.Println(err, "Error while building Transaction")
		return
	}
	if err = dero.Wallet.SendTransaction(tx); err != nil {
		fmt.Println(err, "Error while dispatching Transaction")
		return
	}
	fmt.Println("Dispatched tx", "txid", tx.GetHash().String())
}

func makeIntegratedAddress() (address *rpc.Address) {
	fmt.Println("Creating Integrated Address")

	// Get amount
	value, err := globals.ParseAmount(getText("Enter Amount:"))
	if err != nil {
		fmt.Println(err, "Err parsing amount")
		return
	}

	// Get destination port
	port := uint64(0)
	port, _ = strconv.ParseUint(getText("Enter Port:"), 10, 0)

	// Get comment
	comment := getText("Enter Comment (100 chars max):")

	// Needs a return address?
	needsreplyback := uint64(0)
	if getText("Needs reply-back address? (y/n):") == "y" {
		needsreplyback = 1
	}

	// Amount IA suggests to send
	atomicValueArg := rpc.Argument{
		Name:     rpc.RPC_VALUE_TRANSFER,
		DataType: rpc.DataUint64,
		Value:    value,
	}
	// Port set when sending to a port
	portArg := rpc.Argument{
		Name:     rpc.RPC_DESTINATION_PORT,
		DataType: rpc.DataUint64,
		Value:    uint64(port),
	}
	// Comment up to 100 bytes (should check this...)
	commentArg := rpc.Argument{
		Name:     rpc.RPC_COMMENT,
		DataType: rpc.DataString,
		Value:    comment,
	}
	// Needs a reply back address (the sender's address here...)
	needsReplyBackAddrArg := rpc.Argument{
		Name:     rpc.RPC_NEEDS_REPLYBACK_ADDRESS,
		DataType: rpc.DataUint64,
		Value:    uint64(needsreplyback),
	}
	address, err = rpc.NewAddress(dero.Wallet.GetAddress().String())
	if err != nil {
		fmt.Println(err)
	}
	address.Arguments = rpc.Arguments{
		atomicValueArg,
		portArg,
		commentArg,
		needsReplyBackAddrArg,
	}
	if _, err := address.Arguments.CheckPack(transaction.PAYLOAD0_LIMIT); err != nil {
		fmt.Println(err)
		return
	}

	println("Integrated Address ----------")
	println(address.String())
	fmt.Println("Amount:", globals.FormatMoney(value))
	fmt.Println("Port:", port)
	fmt.Println("Comment:", comment)
	fmt.Println("Needs Reply-Back Address:", needsreplyback)
	println("-----------------------------")
	return address
}

func tokens() {
	if gnomon.TargetHeight == 0 {
		println("Gnomon not running.") // maybe offer scids from the gnomon SC on-chain
		return
	}

	showTokens()
	if getText("Scan for tokens?") != "y" {
		return
	}

	height, err := gnomon.Sqlite.GetLastIndexHeight()
	if err != nil {
		println("Error", err)
	}
	start_height := getText("Start at certain height? This can take a while. Max indexed: " + strconv.Itoa(int(height)))

	scids := gnomon.Sqlite.GetSCIDsByClass([]string{"token"})

	sql.SetReady(false)
	var token_scid string
	var install_height int
	gnomon.Sqlite.DB.QueryRow(
		`SELECT scid,height
		FROM scs
		WHERE height > ?
		ORDER BY height ASC
		LIMIT 1;
		`, start_height).Scan(&token_scid, &install_height)
	sql.SetReady(true)
	if err != nil {
		fmt.Println(err)
	}

	total := len(scids)
	start := false
	counter := 0
	for _, scid := range scids {
		if !start {
			total--
			if token_scid == scid {
				start = true
			} else {
				continue
			}
		}
		counter++
		print("\rScan Progress: ", fmt.Sprintf("%.2f", 1.0/(float64(total)/float64(counter))*100.0), "%")
		scidhash := crypto.HexToHash(scid)
		_, exists := dero.Wallet.GetAccount().Balance[scidhash]
		if exists {
			balance, _, err := dero.Wallet.GetDecryptedBalanceAtTopoHeight(scidhash, -1, dero.Wallet.GetAddress().String())
			if err != nil {
				//Maybe remove or tell user etc.
				dero.Wallet.GetAccount().Balance[scidhash] = balance
			}
			continue
		}
		balance, _, err := dero.Wallet.GetDecryptedBalanceAtTopoHeight(scidhash, -1, dero.Wallet.GetAddress().String())
		if balance != 0 && err == nil {
			if err := dero.Wallet.TokenAdd(scidhash); err != nil {
				fmt.Println(err, "Error adding SCID:", scid)
			} else {
				dero.Wallet.GetAccount().Balance[scidhash] = balance
				fmt.Println("Added", scidhash, "balance:", balance)
			}
		}
	}
	dero.Wallet.Wallet_Memory.Save_Wallet()
	dero.Wallet.Save_Wallet()
}

func showTokens() {

	for scid, _ := range dero.Wallet.GetAccount().Balance {

		if err := dero.Wallet.Sync_Wallet_Memory_With_Daemon_internal(scid); err != nil {
			fmt.Println(err, "Error syncing SCID:", scid.String())
		}
		balance, _, err := dero.Wallet.GetDecryptedBalanceAtTopoHeight(scid, -1, dero.Wallet.GetAddress().String())
		if err != nil {
			dero.Wallet.GetAccount().Balance[scid] = balance
		}
		fmt.Println("Token:", scid.String(), " Balance:", balance)
	}
	//Not sure if this is necessary ...
	dero.Wallet.Wallet_Memory.Save_Wallet()
	dero.Wallet.Save_Wallet()
}
