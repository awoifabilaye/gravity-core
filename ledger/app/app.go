package app

import (
	"context"
	"fmt"

	"github.com/Gravity-Tech/gravity-core/common/adaptors"

	"github.com/Gravity-Tech/gravity-core/ledger/query"

	"github.com/Gravity-Tech/gravity-core/common/state"

	"github.com/Gravity-Tech/gravity-core/common/account"

	"github.com/Gravity-Tech/gravity-core/common/storage"
	"github.com/Gravity-Tech/gravity-core/common/transactions"
	"github.com/Gravity-Tech/gravity-core/ledger/scheduler"

	"github.com/dgraph-io/badger"
	abcitypes "github.com/tendermint/tendermint/abci/types"
)

const (
	Success      uint32 = 0
	Error        uint32 = 500
	NotFoundCode uint32 = 404
)

type OraclesAddresses struct {
	account.ChainType
	account.OraclesPubKey
}
type Genesis struct {
	ConsulsCount              int
	BftOracleInNebulaCount    int
	OraclesAddressByValidator map[account.ConsulPubKey][]OraclesAddresses
}

type GHApplication struct {
	IsSync    bool
	db        *badger.DB
	storage   *storage.Storage
	adaptors  map[account.ChainType]adaptors.IBlockchainAdaptor
	scheduler *scheduler.Scheduler
	ctx       context.Context
	genesis   *Genesis
}

var _ abcitypes.Application = (*GHApplication)(nil)

func NewGHApplication(adaptors map[account.ChainType]adaptors.IBlockchainAdaptor, scheduler *scheduler.Scheduler, db *badger.DB, genesis *Genesis, ctx context.Context) (*GHApplication, error) {
	return &GHApplication{
		db:        db,
		adaptors:  adaptors,
		scheduler: scheduler,
		ctx:       ctx,
		genesis:   genesis,
		storage:   storage.New(),
	}, nil
}

func (app *GHApplication) Info(req abcitypes.RequestInfo) abcitypes.ResponseInfo {
	return abcitypes.ResponseInfo{}
}

func (app *GHApplication) SetOption(req abcitypes.RequestSetOption) abcitypes.ResponseSetOption {
	return abcitypes.ResponseSetOption{}
}

func (app *GHApplication) DeliverTx(req abcitypes.RequestDeliverTx) abcitypes.ResponseDeliverTx {
	tx, err := transactions.UnmarshalJson(req.Tx)
	if err != nil {
		return abcitypes.ResponseDeliverTx{Code: Error}
	}

	err = state.SetState(tx, app.storage, app.adaptors, app.ctx)
	if err != nil {
		return abcitypes.ResponseDeliverTx{Code: Error}
	}
	return abcitypes.ResponseDeliverTx{Code: 0}
}

func (app *GHApplication) CheckTx(req abcitypes.RequestCheckTx) abcitypes.ResponseCheckTx {
	tx, err := transactions.UnmarshalJson(req.Tx)
	if err != nil {
		return abcitypes.ResponseCheckTx{Code: Error}
	}

	mock := *app.storage
	mock.NewTransaction(app.db)
	err = state.SetState(tx, &mock, app.adaptors, app.ctx)
	if err != nil {
		return abcitypes.ResponseCheckTx{Code: Error}
	}

	return abcitypes.ResponseCheckTx{Code: Success}
}

func (app *GHApplication) Commit() abcitypes.ResponseCommit {
	err := app.storage.Commit()
	if err != nil {
		panic(err)
	}
	return abcitypes.ResponseCommit{}
}

func (app *GHApplication) Query(reqQuery abcitypes.RequestQuery) (resQuery abcitypes.ResponseQuery) {
	var err error

	store := storage.New()
	store.NewTransaction(app.db)
	b, err := query.Query(store, reqQuery.Path, reqQuery.Data)
	if err == query.ErrValueNotFound {
		resQuery.Code = NotFoundCode
	} else if err != nil {
		resQuery.Code = Error
	}

	resQuery.Value = b

	return
}

func (app *GHApplication) InitChain(req abcitypes.RequestInitChain) abcitypes.ResponseInitChain {
	app.storage.NewTransaction(app.db)

	err := app.storage.SetBftOracleInNebulaCount(app.genesis.BftOracleInNebulaCount)
	if err != nil {
		panic(err)
	}
	err = app.storage.SetConsulsCount(app.genesis.ConsulsCount)
	if err != nil {
		panic(err)
	}

	var consuls []storage.Consul
	for _, value := range req.Validators {
		var pubKey account.ConsulPubKey
		copy(pubKey[:], value.PubKey.GetData())
		err := app.storage.SetScore(pubKey, uint64(value.Power))
		if err != nil {
			panic(err)
		}
		consuls = append(consuls, storage.Consul{
			PubKey: pubKey,
			Value:  uint64(value.Power),
		})
	}

	err = app.storage.SetConsuls(consuls)
	if err != nil {
		panic(err)
	}

	err = app.storage.SetConsulsCandidate(consuls)
	if err != nil {
		panic(err)
	}

	for validator, value := range app.genesis.OraclesAddressByValidator {
		oracles := make(storage.OraclesByTypeMap)
		for _, oracle := range value {
			oracles[oracle.ChainType] = oracle.OraclesPubKey
		}

		err = app.storage.SetOraclesByConsul(validator, oracles)
		if err != nil {
			panic(err)
		}
	}

	err = app.storage.Commit()
	if err != nil {
		panic(err)
	}

	return abcitypes.ResponseInitChain{}
}

func (app *GHApplication) BeginBlock(req abcitypes.RequestBeginBlock) abcitypes.ResponseBeginBlock {
	app.storage.NewTransaction(app.db)

	err := app.scheduler.HandleBlock(req.Header.Height, app.storage, app.IsSync)
	if err != nil {
		fmt.Printf("Error: %s \n", err.Error())
	}

	return abcitypes.ResponseBeginBlock{}
}

func (app *GHApplication) EndBlock(req abcitypes.RequestEndBlock) abcitypes.ResponseEndBlock {
	err := app.storage.SetLastHeight(uint64(req.Height))
	if err != nil {
		panic(err)
	}

	consuls, err := app.storage.Consuls()
	if err != nil {
		panic(err)
	}

	var newValidators []abcitypes.ValidatorUpdate
	for i := 0; i < app.genesis.ConsulsCount && i < len(consuls); i++ {
		if consuls[i].Value == 0 {
			continue
		}

		pubKey := abcitypes.PubKey{
			Type: "ed25519",
			Data: consuls[i].PubKey[:],
		}

		newValidators = append(newValidators, abcitypes.ValidatorUpdate{
			PubKey: pubKey,
			Power:  int64(consuls[i].Value),
		})
	}

	if len(newValidators) > 0 {
		return abcitypes.ResponseEndBlock{ValidatorUpdates: newValidators}
	} else {
		return abcitypes.ResponseEndBlock{}
	}
}