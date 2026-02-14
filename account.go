package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/deroproject/derohe/cryptography/crypto"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/transaction"
	"github.com/deroproject/derohe/walletapi"
	"golang.org/x/term"
)

// Account managment

// Wallet init
func initializeWalletTable() {
	// init the lookup table one, anyone importing walletapi should init this first, this will take around 1 sec on any recent system
	if os.Getenv("USE_BIG_TABLE") != "" {
		fmt.Printf("Please wait, generating precompute table....")
		walletapi.Initialize_LookupTable(1, 1<<24) // use 8 times more more ram, around 256 MB RAM
		fmt.Printf("done\n")
	} else {
		walletapi.Initialize_LookupTable(1, 1<<21)
	}
}

func openWallet(pass string) (err error) {
	dero.PassHash = sha256.Sum256([]byte(pass))
	if dero.Wallet == nil {
		temp, err := walletapi.Open_Encrypted_Wallet(filepath.Join(dero.Path, dero.WalletName), pass)
		if err != nil {
			fmt.Println("Error opening", filepath.Join(dero.Path, dero.WalletName))
			return err
		}
		dero.Wallet = temp
	} else {
		fmt.Println("Wallet already open, type close to close wallet or exit to close program.", filepath.Join(dero.Path, dero.WalletName))
	}
	return
}

func connectWallet() (err error) {
	if !walletapi.Connected && dero.Wallet != nil {
		fmt.Println("Connecting to:", walletapi.Daemon_Endpoint)
		err := walletapi.Connect(walletapi.Daemon_Endpoint)
		if err != nil {
			fmt.Println("Failed connection attempt to:", walletapi.Daemon_Endpoint)
			fmt.Println("Wallet api Connect error:", err)
		}
	}
	return
}

func checkWallet() {
	if dero.Wallet != nil {
		if !dero.Wallet.IsRegistered() {
			result := getText(`Enter y to start registration if it is hasn't been already:` + dero.Wallet.GetAddress().String())
			if result == "y" {
				register() // consider pausing gnomon during registration
				fmt.Println(`Wallet Registration TX Sent. Checking chain for registration. Waiting...`)
				time.Sleep(20 * time.Second)
				if dero.Wallet.IsRegistered() {
					showAccount(dero.Wallet)
				} else {
					time.Sleep(20 * time.Second)
					if !dero.Wallet.IsRegistered() {
						fmt.Println(`Wait for wallet height to appear, try to check/register again after a few minutes if it remains at 0`)
					}
				}
			}
		} else {
			showAccount(dero.Wallet)
		}
		return
	}
	fmt.Println(`Wallet not open. Enter "open" or "help" for more instructions.`)
}
func showAccount(wallet *walletapi.Wallet_Disk) {
	fmt.Println("Wallet address : ", wallet.GetAddress())
	balance, _ := dero.Wallet.Get_Balance()
	fmt.Println("Balance: ", balance)
	fmt.Println("Dero: ", globals.FormatMoney(balance))
	fmt.Println("Height: ", dero.Wallet.Get_Height())
	fmt.Println("Daemon Height: ", dero.Wallet.Get_Daemon_Height())
	fmt.Println("Timestamp: ", time.Now().Unix())
	if dero.Wallet.IsDaemonOnlineCached() {
		fmt.Println("Daemon: Online")
	} else {
		fmt.Println("Daemon: Offline")
	}
	if !dero.Wallet.IsRegistered() {
		fmt.Println("Unregistered")
	}

	fmt.Println(dero.Wallet)
	if !wallet.IsRegistered() {
		fmt.Println("Unregistered")
	}
}

func register() {
	fmt.Println(dero.Wallet.GetAddress().String() + " is going to be registered. Please wait 'til the account is registered. This is a pre-condition POW for using the online chain.")
	fmt.Println("It may take a little while to register the address on the blockchain, make sure to register only once.")
	fmt.Println("This will take a couple of minutes...")

	var reg_tx *transaction.Transaction

	successful_regs := make(chan *transaction.Transaction)

	counter := 0

	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go func() {
			for counter == 0 {
				lreg_tx := dero.Wallet.GetRegistrationTX()
				hash := lreg_tx.GetHash()
				if hash[0] == 0 && hash[1] == 0 && hash[2] == 0 {
					successful_regs <- lreg_tx
					counter++
					break
				}
			}
		}()
	}

	reg_tx = <-successful_regs

	fmt.Println("Registration TXID", reg_tx.GetHash())
	err := dero.Wallet.SendTransaction(reg_tx)
	if err != nil {
		fmt.Println("Registration TX send error: ", err)
	} else {

		fmt.Println("Registration TX sent successfully")
	}
}

// Creates a new wallet with supplied password, asks for wallet name then saves and closes it
func createWallet(password string) (address, seed string, ok bool) {
	dero.Path = getBasePath()
	dero.WalletName = getText(`Enter DB Name for New Account (eg. wallet.db):`)
	if fileExists(filepath.Join(dero.Path, dero.WalletName)) {
		fmt.Println("Error: " + dero.WalletName + " already exists.")
		return
	}
	dero.PassHash = sha256.Sum256([]byte(password))
	temp, err := walletapi.Create_Encrypted_Wallet_Random(filepath.Join(dero.Path, dero.WalletName), password)
	if err != nil {
		fmt.Println(err, "Error occured while creating new wallet.")
		dero.Wallet = nil
		return
	}
	dero.Wallet = temp
	dero.Wallet.SetNetwork(true) //set to mainnet
	dero.Wallet.SetSeedLanguage("English")
	address = dero.Wallet.GetAddress().String()
	seed = dero.Wallet.GetSeed()
	dero.Wallet.Close_Encrypted_Wallet()
	dero.Wallet = nil

	fmt.Println("New Wallet File:", dero.WalletName)
	fmt.Println("New Wallet Address Generated:", address)
	fmt.Println("Seed", seed)
	ok = true
	return
}

// Restores wallet from seed phrase and proceeds to open wallet
func recoverFromSeed() (ok bool) {
	dero.Path = getBasePath()
	dero.WalletName = getText(`Enter DB Name:`)
	if fileExists(filepath.Join(dero.Path, dero.WalletName)) {
		println("Error: File already exists.")
		return
	}
	password := getPassword(`Enter Password:`)
	electrum_words := getText(`Enter 25 word seed phrase:`)
	temp, err := walletapi.Create_Encrypted_Wallet_From_Recovery_Words(filepath.Join(dero.Path, dero.WalletName), password, electrum_words)
	if err != nil {
		fmt.Println(err, "Error while recovering wallet using seed.")
		return
	}
	dero.Wallet = temp
	temp = nil
	println("Wallet recovered from seed words. Wallet file is saved:", filepath.Join(dero.Path, dero.WalletName))

	walletapi.Daemon_Endpoint = getDaemonAddress()
	common_processing(dero.Wallet)
	go walletapi.Keep_Connectivity() // maintain connectivity
	err = connectWallet()
	if err != nil {
		println("error:", err)
	}
	gnomon_updates_enabled = false
	println("Waiting a few seconds...")
	time.Sleep(5 * time.Second)
	// disable gnomon updates when logged into wallet
	return true
}

// Restores wallet from seed phrase and proceeds to open wallet
func recoverFromHex() (ok bool) {
	dero.Path = getBasePath()
	dero.WalletName = getText(`Enter DB Name:`)
	if fileExists(filepath.Join(dero.Path, dero.WalletName)) {
		println("Error: File already exists.")
		return
	}
	password := getPassword(`Enter Password:`)
	seed_key_string := getText(`Enter your seed (hex 64 chars):`)

	seed_raw, err := hex.DecodeString(seed_key_string)
	if len(seed_key_string) >= 65 || err != nil {
		println(err, "Seed must be less than 66 chars hexadecimal chars")
		return
	}

	wallett, err := walletapi.Create_Encrypted_Wallet(filepath.Join(dero.Path, dero.WalletName), password, new(crypto.BNRed).SetBytes(seed_raw))
	if err != nil {
		println(err, "Error while recovering wallet using seed key")
		return
	}
	println("Wallet recovered from hex seed. Wallet file is saved:", filepath.Join(dero.Path, dero.WalletName))
	dero.Wallet = wallett
	wallett = nil

	dero.Wallet.SetSeedLanguage("English")
	println("Seed", "English")
	showSeed(dero.Wallet)
	walletapi.Daemon_Endpoint = getDaemonAddress()
	common_processing(dero.Wallet)
	go walletapi.Keep_Connectivity() // maintain connectivity
	err = connectWallet()
	if err != nil {
		println("error:", err)
	}
	gnomon_updates_enabled = false
	println("Waiting a few seconds...")
	time.Sleep(5 * time.Second)
	// disable gnomon updates when logged into wallet
	return true

}

// Display seed 25 word seed phrase
func showSeed(wallet *walletapi.Wallet_Disk) {
	seed := wallet.GetSeed()
	fmt.Println("PLEASE NOTE: the following 25 words can be used to recover access to your wallet. Please write them down and store them somewhere safe and secure. Please do not store them in your email or on file storage services outside of your immediate control.")
	fmt.Println(seed)
}

// Update password
func changePassword() {
	if !checkPass(getText("Enter existing password to change to a new one:")) {
		fmt.Println("Incorrect password, not updated")
	}
	new_password := getText("Enter new password:")
	if new_password == "" {
		fmt.Println("Password not updated")
	}
	if "y" != getText(`Enter y to confirm password update to: "`+new_password) {
		return
	}
	err := dero.Wallet.Set_Encrypted_Wallet_Password(new_password)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println("Password updated successfully")
		fmt.Println("")
	}
}

// Used for locating wallet file
func getBasePath() (data_directory string) {
	data_directory = globals.GetDataDirectory() //should be mainnet / testnet etc set by cli args from globals
	//	fmt.Println(data_directory)
	if data_directory == "" {
		var err error
		data_directory, err = os.Getwd()
		if err != nil {
			fmt.Printf("Error getting directory, using temp dir err %s\n", err)
			data_directory = os.TempDir()
		}
	}
	return
}

func fileExists(location string) bool {
	if _, err := os.Stat(location); !errors.Is(err, os.ErrNotExist) && dero.WalletName != "" {
		fmt.Println(filepath.Join(dero.Path, dero.WalletName), " already exists.")
		return true
	}
	return false
}

var session_duration = time.Minute * 60 * 4 //4 hours default
var session_expires = time.Now().Add(session_duration)
var hidePass = true

func getPassword(text string) (password string) {

	Mutex.Lock()
	updates_enabled = false
	Mutex.Unlock()
	if hidePass {
		fmt.Println(text)
		bytePassword, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			fmt.Println("Error reading password:", err)
			return
		}
		fmt.Println()
		password = string(bytePassword)
	} else {
		password = getText(text)
	}
	Mutex.Lock()
	updates_enabled = true
	defer Mutex.Unlock()
	// don't allow null passwords for now
	if len(password) == 0 {
		fmt.Println("Password can't be empty.")
		return
	}
	fmt.Println("Verifying...")
	return
}

func handleSession(text string) string {
	// Handle sessions on input.
	if sessionExpired() && text != "exit" && text != "close" {
		// session expired, save current command and check password
		if login(getPassword("Enter password: ")) {
			return text
		} else {
			return ""
		}
	}
	return text
}

// Check password against saved hash
func login(pass string) bool {
	ok := false
	if dero.Wallet == nil {
		return true
	}
	if checkPass(pass) {
		sessionUpdate()
		return true
	}
	return ok
}

// Check password against saved has
func checkPass(pass string) (ok bool) {
	if sha256.Sum256([]byte(pass)) == dero.PassHash {
		ok = true
		sessionUpdate()
	}
	return
}

func sessionExpired() bool {
	if time.Now().After(session_expires) && dero.Wallet != nil {
		return true
	}
	return false
}
func sessionUpdate() {
	session_expires = time.Now().Add(session_duration)
}
func setPassHidden() {
	if getText(`Hide password while entering? (y/n)`) != "n" {
		println("Hiding pass")
		hidePass = true
		return
	}
	println("Showing pass (less secure)")
	hidePass = false
}

// Wallet Account helpers
func cleanWallet(wallet *walletapi.Wallet_Disk) {
	if wallet.GetMode() {
		wallet.Clean()
	}
}
