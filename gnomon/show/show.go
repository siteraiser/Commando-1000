package show

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"gnomon/structs"

	_ "github.com/mattn/go-sqlite3"
)

var Mutex sync.Mutex

// Display mode selector
var DisplayMode = 0
var Events = make(chan Message)

// Store block number to display for when there is no new height to use
var status = struct {
	block int64
}{
	block: 0,
}

type Message struct {
	Text    string
	ofType  string
	Vars    []any
	Err     error
	ShowNow bool //for standalone mode, show now instead of waiting for next block
}

var messages = []Message{}

// Print through this so that it can be coordinated with the large display
func NewMessage(message Message) {
	Mutex.Lock()

	if DisplayMode == -1 {
		Events <- message
	} else {
		if message.ShowNow {
			fmt.Println(message)
		} else {
			messages = append(messages, message)
		}
	}
	Mutex.Unlock()
}

var PreferredRequests *int8
var Status *structs.State

// Prints out the block being requested and other stats needed for the display mode selected
func ShowBlockStatus(bheight int64, s int, text string) {
	if DisplayMode == -1 {
		messages = []Message{}
		return
	}
	Mutex.Lock()
	if bheight != -1 {
		status.block = bheight
	}
	for _, msg := range messages {
		vs := []any{msg.Text}
		for _, v := range msg.Vars {
			vs = append(vs, v)
		}
		fmt.Println(vs)
	}
	skipreturn := false
	if len(messages) > 0 && DisplayMode > 0 {
		skipreturn = true
	}
	messages = []Message{}

	speedms := "0"
	speedbph := "0"

	if s != 0 {
		speedms = strconv.Itoa(s)
		speedbph = strconv.Itoa((1000 / s) * 60 * 60)
	}

	show := ""
	if DisplayMode == 0 || DisplayMode == 1 {

		if DisplayMode != 1 {
			show = "Block:" + strconv.Itoa(int(status.block))
		}
		show += " " + text +
			" Speed:" + speedms + "ms" +
			" " + speedbph + "bph" +
			" Total Errors:" + strconv.Itoa(int(Status.TotalErrors)) + "                 "
		if DisplayMode == 0 {
			fmt.Print("\r", show)
		}
	}
	if DisplayMode == 1 || DisplayMode == 2 {
		bigDisplay(status.block, show, skipreturn)
	}
	Mutex.Unlock()
}
func bigDisplay(n int64, show string, skipreturn bool) {
	chars := []int{}
	ns := strconv.FormatInt(n, 10)
	for _, ch := range ns {
		integer, _ := strconv.Atoi(string(ch))
		chars = append(chars, integer)
	}
	lines := []string{}
	for l := 0; l < 6; l++ {
		line := ""
		for i, r := range chars {
			if i == 0 {
				line += logo[l]
			}
			line += " " + numbers[r][l] + " "
		}
		lines = append(lines, line)
	}
	pad := " " + strings.Repeat(" ", len(lines[0])) + " "
	moveup := "8"
	if show != "" {
		moveup = "9"
	}
	if !skipreturn {
		fmt.Print("\033[" + moveup + "A")
	}

	fmt.Printf(pad + " \n")
	fmt.Printf("   ______" + pad + " \n")
	fmt.Printf(" %v \n", lines[0])
	fmt.Printf(" %v \n", lines[1])
	fmt.Printf(" %v \n", lines[2])
	fmt.Printf(" %v \n", lines[3])
	fmt.Printf(" %v \n", lines[4])
	fmt.Printf(" %v \n", lines[5])

	if show != "" {
		fmt.Printf(pad + " \n")
		fmt.Printf(show)
	}
}

var numbers = [10][6]string{
	[6]string{
		" 00 ",
		"0  0",
		"0 00",
		"00 0",
		"0  0",
		" 00 ",
	},
	[6]string{
		"  0 ",
		"0 0 ",
		"  0 ",
		"  0 ",
		"  0 ",
		"0000",
	},
	[6]string{
		" 00 ",
		"0  0",
		"  0 ",
		" 0  ",
		"0   ",
		"0000",
	},
	[6]string{
		"0000",
		"   0",
		"  0 ",
		" 000",
		"   0",
		"000 ",
	},
	[6]string{
		"  00",
		" 0 0",
		"0  0",
		"0000",
		"   0",
		"   0",
	},
	[6]string{
		"0000",
		"0   ",
		"000 ",
		"   0",
		"0  0",
		" 00 ",
	},
	[6]string{
		" 00 ",
		"0  0",
		"0   ",
		"000 ",
		"0  0",
		" 00 ",
	},
	[6]string{
		"0000",
		"0  0",
		"  0 ",
		" 00 ",
		" 0  ",
		"00  ",
	},
	[6]string{
		" 00 ",
		"0  0",
		" 00 ",
		"0  0",
		"0  0",
		" 00 ",
	},
	[6]string{
		" 00 ",
		"0  0",
		" 000",
		"   0",
		"0  0",
		" 00 ",
	},
}

var logo = [6]string{
	` / ____ \   `,
	`D / __ \ \  `,
	`E/ /  \ \ \ `,
	`R\ \  / / / `,
	`O \_||_/ /  `,
	`G N O M O N `,
}
