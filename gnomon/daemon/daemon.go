package daemon

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"gnomon/show"
	"gnomon/structs"

	"github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/walletapi"
	"github.com/ybbus/jsonrpc/v3"
)

var Endpoints = []Connection{
	//{Address: "node.derofoundation.org:11012"},
	{Address: "dero-node-ch4k1pu.mysrv.cloud"},
	{Address: "64.226.81.37:10102"},
}
var Mutex sync.Mutex

// No Errors
func OK() bool {
	if Status.DbOk && Status.ApiOk {
		return true
	}
	return false
}

// Error type, name, details
func NewError(einfo ...any) {
	Status.Mutex.Lock()
	switch einfo[0] {
	case "database":
		Status.DbOk = false
	case "connection", "rpc":
		Status.ApiOk = false
	}
	Status.ErrorCount++
	Status.ErrorType = einfo[0].(string)
	Status.ErrorName = einfo[1].(string)
	if len(einfo) == 3 {
		Status.ErrorDetail = einfo[2].(string)
	}
	if len(einfo) == 4 {
		Status.ErrorDetail = einfo[2].(string)
		Status.Error = einfo[3].(error)
	}
	Status.Mutex.Unlock()
}

// Reset Errors
func Reset() {
	Status.TotalErrors += Status.ErrorCount
	Status.ErrorCount = 0
	Status.ErrorType = ""
	Status.ErrorName = ""
	Status.ErrorDetail = ""
	Status.Error = nil
	Status.DbOk = true
	Status.ApiOk = true
	Status.Paused = false
}

// Error type, name, details
func Pause() {
	Status.Mutex.Lock()
	Status.Paused = true
	Status.Mutex.Unlock()
}
func UnPause() {
	Status.Mutex.Lock()
	Status.Paused = false
	Status.Mutex.Unlock()
}
func Paused() bool {
	return Status.Paused
}

var Status = &structs.State{
	ErrorType:   "",
	ErrorName:   "",
	ErrorDetail: "",
	Error:       nil,
	DbOk:        true,
	ApiOk:       true,
	Paused:      false,
	ErrorCount:  0,
	TotalErrors: 0,
}

type Connection struct {
	Id      uint8
	Address string
	Errors  []error
}

var currentEndpoint Connection //= Endpoints[0]

type Block struct {
	Height    int64
	TxIds     []string
	Processed bool
}

type Batch struct {
	Id        int
	TxIds     []string
	Processed bool
}

func BlockByHeight(height int64) *Block {
	for i, block := range Blocks {
		if block.Height == height {
			return &Blocks[i]
		}
	}
	return &Block{}
}

func ProcessBlocks(txid string) {
	for i, _ := range Blocks {
		if len(Blocks[i].TxIds) != 0 {
			bindex := slices.Index(Blocks[i].TxIds, txid)
			if bindex != -1 && bindex < len(Blocks[i].TxIds) { //bindex +1 <
				Blocks[i].TxIds = append(Blocks[i].TxIds[:bindex], Blocks[i].TxIds[bindex+1:]...)
			}
		}

		if len(Blocks[i].TxIds) == 0 {
			Blocks[i].Processed = true
		}
	}
}

var Batches []Batch
var BatchCount = 0
var Blocks []Block
var TXIDSProcessing []string

func RemoveBlocks(bheight int) {
	var newlist []Block
	for _, block := range Blocks {
		if block.Height != int64(bheight) {
			newlist = append(newlist, block)
		}
	}
	Blocks = newlist
}

func AllTXs() (all []string) {
	for _, block := range Blocks {
		all = append(all, block.TxIds...)
	}
	return
}
func RemoveTXs(txids []string) {

	var blocklist []Block
	for _, block := range Blocks {
		txlist := []string{}
		for _, txid := range txids {
			if len(block.TxIds) != 0 {
				if !slices.Contains(block.TxIds, txid) {
					txlist = append(txlist, txid)
				}
			}
		}
		blocklist = append(blocklist, Block{
			Height:    block.Height,
			TxIds:     txlist,
			Processed: block.Processed,
		})
	}
	Blocks = blocklist

}

func RemoveTXIDs(txids []string) {
	var newlist []string
	for _, txid := range TXIDSProcessing {
		if slices.Index(txids, txid) == -1 {
			newlist = append(newlist, txid)
		}
	}
	TXIDSProcessing = newlist
}

/*
** Daemon connection / request management
 */
// The indexer is asking if the request queue is low enough
func Ask(use string) {
	if !OK() {
		return
	}
	for {
		Mutex.Lock()
		exceeded := 0
		totouts := 0
		if use == "height" {
			totouts, exceeded = outs(HeightOuts)
		} else if use == "tx" {
			totouts, exceeded = outs(TxOuts)
		} else if use == "sc" {
			totouts, exceeded = outs(SCOuts)
		}
		if exceeded != totouts && isReady(use) {
			Mutex.Unlock()
			return
		}
		Mutex.Unlock()
	}
}

// Determines if the scheduled time has been reached
func isReady(use string) bool {
	if Smoothing == 0 {
		return true
	}
	ok := true
	if use == "height" {
		//currentEndpoint = selectEndpoint("DERO.GetBlock")
		if time.Now().After(sheduledb[currentEndpoint.Id]) {
			ok = true
		}
	} else if use == "tx" {
		//currentEndpoint = selectEndpoint("DERO.GetTransaction")
		if time.Now().Add(time.Millisecond).After(sheduledt[currentEndpoint.Id]) {
			ok = true
		}
	} else if use == "sc" {
		//currentEndpoint = selectEndpoint("DERO.GetSC")
		if time.Now().After(sheduleds[currentEndpoint.Id]) {
			ok = true
		}
	}
	/*if use == "tx" || use == "sc" {
		if BatchCount < 100 {
			ok = false
		}

	}
	*/
	return ok
}

var HeightOuts []uint8
var TxOuts []uint8
var SCOuts []uint8

// Returns the total amount of active request counters and if there are more than there needs to be
func outs(Outs []uint8) (int, int) {
	totouts := 0
	exceeded := 0
	for id, out := range Outs {
		if len(Endpoints[id].Errors) == 0 {
			totouts++
			if out > uint8(PreferredRequests)/2 {
				exceeded++
			}
		}
	}
	return totouts, exceeded
}

func FindLowestHeight(start int64, top int64) int64 {
	var starts = []int64{}
	for i, endpoint := range Endpoints {
		if len(Endpoints[i].Errors) != 0 {
			continue
		}
		currentEndpoint = endpoint
		starts = append(starts, FindStart(start, top))
	}
	return slices.Max(starts)
}

// Check indexable height at daemon
func FindStart(start int64, top int64) (block int64) {
	difference := top - start
	offset := difference / 2
	if top-start == 1 {
		return top - 1
	}
	if GetBlockInfo(rpc.GetBlock_Params{Height: uint64(block)}).Status == "OK" {
		return FindStart(start, offset+start)
	} else {
		return FindStart(offset+start, top)
	}
}

var Standalone = true

// Check supplied connections, manage errors and intitialize request counters
func AssignConnections(iserror bool) {
	HeightOuts = HeightOuts[0:0]
	TxOuts = TxOuts[0:0]
	SCOuts = SCOuts[0:0]

	for i, endpoint := range Endpoints {
		lasterrcnt := len(Endpoints[i].Errors)
		var result any
		var rpcClient jsonrpc.RPCClient
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		nodeaddr := "http://" + endpoint.Address + "/json_rpc"
		show.NewMessage(show.Message{Text: "Testing: ", Vars: []any{nodeaddr}, ShowNow: true})
		//fmt.Println("Testing:", nodeaddr)
		//show.NewMessage(show.Message{Text: "Testing: ", Vars: []any{nodeaddr}})
		rpcClient = jsonrpc.NewClient(nodeaddr)
		err := rpcClient.CallFor(ctx, &result, "DERO.GetInfo") //, params no params argument
		Endpoints[i].Id = uint8(i)
		HeightOuts = append(HeightOuts, 0)
		TxOuts = append(TxOuts, 0)
		SCOuts = append(SCOuts, 0)
		if err != nil {
			show.NewMessage(show.Message{Text: "Error endpoint: ", Vars: []any{endpoint}, Err: err, ShowNow: true})
			//fmt.Println("Error endpoint:", endpoint, " Error:", err)
			//show.NewMessage(show.Message{Text: "Error endpoint: ", Vars: []any{endpoint}, Err: err})
			Endpoints[i].Errors = []error{err}
		} else if lasterrcnt == 1 {
			Endpoints[i].Errors = []error{}
		}
	}
	ecount := 0
	for _, endpoint := range Endpoints {
		if len(endpoint.Errors) != 0 {
			ecount++
		}
	}
	if len(Endpoints) == ecount {
		for i := range Endpoints {
			Endpoints[i].Errors = []error{}
		}
		show.NewMessage(show.Message{Text: "Retrying connections", ShowNow: true})
		//fmt.Println("Retrying connections")
		w, _ := time.ParseDuration("10s")
		time.Sleep(w)
		AssignConnections(false)
	}
	Reset()
}
func InitEndpoint() {
	currentEndpoint = Endpoints[0]
}

var sheduledb = make(map[uint8]time.Time)
var sheduledt = make(map[uint8]time.Time)
var sheduleds = make(map[uint8]time.Time)

// Set the next time that is available for sending
func schedule(method string, endpoint Connection, wait time.Duration) {
	if sheduledb[endpoint.Id].Year() < 2000 {
		sheduledb[endpoint.Id] = time.Now()
		sheduledt[endpoint.Id] = time.Now()
		sheduleds[endpoint.Id] = time.Now()
	}
	if method == "DERO.GetBlock" {
		sheduledb[endpoint.Id].Add(wait / 2)
	} else if method == "DERO.GetTransaction" {
		sheduledt[endpoint.Id].Add(wait)
	} else if method == "DERO.GetSC" {
		sheduleds[endpoint.Id].Add(wait)
	}
}

var priorGBTimes = make(map[uint8][]int64)
var priorTxTimes = make(map[uint8][]int64)
var priorSCTimes = make(map[uint8][]int64)

// Returns an adjusted average wait time
func waitTime(method string, endpoint Connection) (time.Time, time.Duration) {
	avgspeed := 0
	gtxtime := time.Time{}
	gtxtime = time.Now()
	avgspeed = calculateSpeed(endpoint.Id, method)
	ratio := 1.0
	var waittime = time.Microsecond * time.Duration(int(int(float64(avgspeed)/float64(ratio))))
	return gtxtime, waittime
}

// Returns the average wait time
func calculateSpeed(id uint8, method string) int {
	var priorTimes map[uint8][]int64
	if method == "DERO.GetBlock" {
		priorTimes = priorGBTimes
	} else if method == "DERO.GetTransaction" {
		priorTimes = priorTxTimes
	} else if method == "DERO.GetSC" {
		priorTimes = priorSCTimes
	}
	total := int64(0)
	for _, ti := range priorTimes[id] {
		total += ti
	}
	value := int64(0)
	if len(priorTimes[id]) != 0 {
		value = int64(total) / int64(len(priorTimes[id]))
	}
	return int(value)
}

var Smoothing = 0

// Individually saves the last wait time for the requests (might be better to test individually to have a more accurate measurement)
func updateSpeed(id uint8, method string, start time.Time) {
	var priorTimes = make(map[uint8][]int64)
	if method == "DERO.GetBlock" {
		priorTimes = priorGBTimes
	} else if method == "DERO.GetTransaction" {
		priorTimes = priorTxTimes
	} else if method == "DERO.GetSC" {
		priorTimes = priorSCTimes
	}

	if len(priorTimes[id]) > Smoothing {
		priorTimes[id] = priorTimes[id][Smoothing:]
	}
	priorTimes[id] = append(priorTimes[id], time.Since(start).Microseconds())
}

// Returns the handle for the corresponding counts
func getOutsByMethod(method string) []uint8 {
	if method == "DERO.GetBlock" {
		return HeightOuts
	} else if method == "DERO.GetTransaction" {
		return TxOuts
	} else if method == "DERO.GetSC" {
		return SCOuts
	}
	return []uint8{}
}

var PreferredRequests = int8(0)

// Tries to select the endpoint with the fewest en-route requests
func selectEndpoint(method string) Connection {

	var Outs []uint8
	endpc := 0
	Outs = getOutsByMethod(method)
	endpc = len(Outs)
	endpoint := Connection{}
	endpoint = currentEndpoint

	if len(Outs) != 0 {
		if Outs[endpoint.Id] >= uint8(PreferredRequests) && endpc > 1 {
			for out := range endpc {
				eid := uint8(out)
				if eid != endpoint.Id && Outs[eid] < uint8(PreferredRequests) && len(Endpoints[eid].Errors) == 0 {
					endpoint = Endpoints[eid]
				}
			}
			if currentEndpoint.Id != endpoint.Id && Outs[endpoint.Id] < uint8(PreferredRequests) {
				currentEndpoint = endpoint
			}
		}
	}
	return endpoint
}

// var Cancels = map[int]context.CancelFunc{}
// var cancelids = 0

func callRPC[t any](method string, params any, validator func(t) bool) t {
	if !OK() {
		var zero t
		return zero
	}
	result, err := getResult[t](method, params)
	if err != nil {
		var zero t
		return zero
	}
	if !validator(result) {
		show.NewMessage(show.Message{Text: method + " failed validation", Err: errors.New("failed validation"), ShowNow: true})
		var zero t
		return zero
	}
	return result
}
func call[T any](rpcClient jsonrpc.RPCClient, params any, method string, result *T) error {
	if params == nil {
		return rpcClient.CallFor(context.Background(), &result, method)
	}
	return rpcClient.CallFor(context.Background(), &result, method, params)
}
func getResult[T any](method string, params any) (T, error) {
	var result T
	var rpcClient jsonrpc.RPCClient
	var endpoint Connection
	var ctx context.Context
	var gtxtime time.Time
	//var thiscancel = 0

	Mutex.Lock()
	/*
		cancelids++
		thiscancel = cancelids
		ctx, Cancels[thiscancel] = context.WithTimeout(context.Background(), 60*time.Second) //Cancels[thiscancel]
		defer Cancels[thiscancel]()
	*/
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) //Cancels[thiscancel]
	defer cancel()

	endpoint = selectEndpoint(method)

	var wait time.Duration
	if Smoothing != 0 {
		gtxtime, wait = waitTime(method, endpoint)
		schedule(method, endpoint, wait)
	}
	//	if time.Now().Before(sheduled) {
	Outs := getOutsByMethod(method)
	if len(Outs) != 0 {
		Outs[endpoint.Id]++
	}

	Mutex.Unlock()
	//	}

	done := make(chan error, 1)

	// Make a call to rpc

	go func() {
		Mutex.Lock()
		nodeaddr := "http://" + endpoint.Address + "/json_rpc"
		rpcClient = jsonrpc.NewClient(nodeaddr)
		Mutex.Unlock()
		done <- call[T](rpcClient, params, method, &result) //rpcClient.CallFor(context.Background(), &result, method)

	}()

	select {
	case <-ctx.Done():
		Mutex.Lock()
		/*
			delete(Cancels, thiscancel)
			for i := range Cancels {
				Cancels[i]()
				delete(Cancels, i)
			}
		*/
		var zero T
		Outs := getOutsByMethod(method)
		if len(Outs) != 0 {
			Outs[endpoint.Id]--
		} /* else {
			return zero, errors.New("No outs")
		}*/
		Mutex.Unlock()
		show.NewMessage(show.Message{Text: "RPC Method:" + method, Err: errors.New("RPC timed out:"), ShowNow: true})
		NewError("rpc", method, endpoint.Address, errors.New("RPC timed out"))

		return zero, errors.New("RPC timed out")
	case err := <-done:
		Mutex.Lock()
		//delete(Cancels, thiscancel)
		Outs := getOutsByMethod(method)
		if len(Outs) != 0 {
			Outs[endpoint.Id]--
		}
		if Smoothing != 0 {
			notime := time.Time{}
			if gtxtime != notime {
				updateSpeed(endpoint.Id, method, gtxtime)
			}
		}
		Mutex.Unlock()

		if err != nil {

			if strings.Contains(err.Error(), "-32098") && strings.Contains(err.Error(), "mismatch") { //Tx statement roothash mismatch ref blid... skip it
				fmt.Println(err)
				var zero T
				return zero, err
			} else if strings.Contains(err.Error(), "-32098") && strings.Contains(err.Error(), "many parameters") { //Using batching now so this shouldn't occur
				fmt.Println(err)
				log.Fatal("Daemon is not compatible (" + endpoint.Address + ")")
			} else if strings.Contains(err.Error(), "wsarecv: A connection attempt failed("+endpoint.Address+")") {
				//maybe handle connection errors here with a cancel / rollback instead.
				NewError("connection", method, endpoint.Address, err)
				fmt.Println(err)
				//	log.Fatal(err)
			} else {
				if !strings.Contains(err.Error(), "200") {
					NewError("rpc", method, endpoint.Address, err)
				}
			}

		}
		return result, nil
	}

}

func GetTopoHeight() int64 {
	validator := func(r rpc.GetInfo_Result) bool {
		return r.TopoHeight != 0
	}
	result := callRPC("DERO.GetInfo", nil, validator)
	return result.TopoHeight
}

func GetTransaction(params rpc.GetTransaction_Params) rpc.GetTransaction_Result {
	validator := func(r rpc.GetTransaction_Result) bool {
		if r.Status == "" {
			//fmt.Println(r)
		}
		return r.Status != ""
	}
	result := callRPC("DERO.GetTransaction", params, validator)
	return result
}

func GetBlockInfo(params rpc.GetBlock_Params) rpc.GetBlock_Result {
	validator := func(r rpc.GetBlock_Result) bool {
		return r.Block_Header.Depth != 0 //false //
	}
	result := callRPC("DERO.GetBlock", params, validator)
	return result
}

func GetTxPool() rpc.GetTxPool_Result {
	validator := func(r rpc.GetTxPool_Result) bool {
		return r.Status != ""
	}
	result := callRPC("DERO.GetTxPool", nil, validator)
	return result
}

func GetDaemonInfo() rpc.GetInfo_Result {
	validator := func(r rpc.GetInfo_Result) bool {
		return r.TopoHeight != 0
	}
	result := callRPC("DERO.GetInfo", nil, validator)
	return result
}

func GetSC(scParam rpc.GetSC_Params) rpc.GetSC_Result {
	validator := func(r rpc.GetSC_Result) bool {
		if scParam.Code {
			return r.Code != ""
		}
		return true
	}
	result := callRPC("DERO.GetSC", scParam, validator)
	return result
}

func GetSCCode(scid string) rpc.GetSC_Result {
	return GetSC(rpc.GetSC_Params{
		SCID:       scid,
		Code:       true,
		Variables:  false,
		TopoHeight: walletapi.Get_Daemon_Height(),
	})
}

func GetSCValues(scid string) rpc.GetSC_Result {
	return GetSC(rpc.GetSC_Params{
		SCID:       scid,
		Code:       false,
		Variables:  true,
		TopoHeight: walletapi.Get_Daemon_Height(),
	})
}

func GetSCIDImage(keys map[string]interface{}) image.Image {
	// simple way to set timeouts
	const timeout = time.Second * 9 // the world is a really big place
	for k, v := range keys {
		if !strings.Contains(k, "image") && !strings.Contains(k, "icon") {
			continue
		}
		encoded := v.(string)
		b, e := hex.DecodeString(encoded)
		if e != nil {
			fmt.Println(e, encoded)
			continue
		}
		value := string(b)
		fmt.Println("scid", "key", k, "value", value)

		//	furi, err := storage.ParseURI(value)
		//	fmt.Println("storage.ParseURI:", furi)

		uri, err := url.Parse(value) //storage.ParseURI(value)
		fmt.Println("url.Parse:", uri)
		if err != nil {
			fmt.Println(err, value)
			return nil
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, "GET", uri.String(), nil)
			if err != nil {
				fmt.Println(err, "get error")
				return nil
			}
			client := http.DefaultClient
			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				return nil
			} else {
				defer resp.Body.Close()
				i, _, err := image.Decode(resp.Body)
				if err != nil {
					return nil
				}
				return i
			}
		}
	}
	return nil
}
func GetSCNameFromVars(keys map[string]interface{}) string {
	var text string

	for k, v := range keys {
		if !strings.Contains(k, "name") {
			continue
		}

		var e error
		var b []byte
		switch v := v.(type) {
		case string:
			b, e = hex.DecodeString(v)
			if e != nil {
				continue
			}
		default:
			continue // what else can we do ?
		}

		text = string(b)
		//	fmt.Println("Name found:", text)
	}
	if text == "" {
		return ""
	}
	return text
}
func GetSCDescriptionFromVars(keys map[string]interface{}) string {
	var text string

	for k, v := range keys {
		if !strings.Contains(k, "descr") {
			continue
		}
		b, e := hex.DecodeString(v.(string))
		if e != nil {
			continue // what else can we do ?
		}
		text = string(b)
	}
	if text == "" {
		return ""
	}
	return text
}

func GetSCIDImageURLFromVars(keys map[string]interface{}) string {
	var text string

	for k, v := range keys {
		if !strings.Contains(k, "imageurl") {
			continue
		}
		b, e := hex.DecodeString(v.(string))
		if e != nil {
			continue // what else can we do ?
		}
		text = string(b)
	}
	if text == "" {
		return ""
	}
	return text
}
func GetBlockDeserialized(blob string) block.Block {

	var bl block.Block
	b, err := hex.DecodeString(blob)
	if err != nil {
		// should probably log or handle this error
		fmt.Println(err.Error())
		return block.Block{}
	}
	bl.Deserialize(b)
	return bl
}
