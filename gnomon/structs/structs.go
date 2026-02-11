package structs

import (
	"sync"

	"github.com/deroproject/derohe/rpc"
)

type SCIDVariable struct {
	Key   any
	Value any
}

type FastSyncImport struct {
	Signer   string
	Height   uint64
	SCName   string
	SCDesc   string
	SCImgURL string
}

type SCIDToIndexStage struct {
	Type       string
	TXHash     string
	Fsi        *FastSyncImport
	ScVars     []*SCIDVariable
	ScCode     string
	Params     rpc.GetSC_Params
	Entrypoint string
	Class      string
	Tags       string
}

type State struct {
	ErrorType   string
	ErrorName   string
	ErrorDetail string
	Error       error
	DbOk        bool
	ApiOk       bool
	ErrorCount  int64
	TotalErrors int64
	Paused      bool
	OK          interface {
		OK() bool
	}
	Reset interface {
		Reset()
	}
	Errors interface {
		NewError(etype string, ename string)
	}
	sync.Mutex
}
