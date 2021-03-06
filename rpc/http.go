package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Gravity-Tech/gravity-core/common/account"

	"github.com/Gravity-Tech/gravity-core/common/storage"

	"github.com/Gravity-Tech/gravity-core/common/transactions"
)

var cfg *Config

type SetNebulaRq struct {
	NebulaId             string
	ChainType            string
	MaxPulseCountInBlock uint64
	MinScore             uint64
}
type VotesRq struct {
	Votes []VoteRq
}
type VoteRq struct {
	PubKey string
	Score  uint64
}

func ListenRpcServer(config *Config) {
	cfg = config
	http.HandleFunc("/vote", vote)
	http.HandleFunc("/setNebula", setNebulaHandler)
	err := http.ListenAndServe(cfg.Host, nil)
	if err != nil {
		fmt.Printf("Error Private RPC: %s", err.Error())
	}
}

func vote(w http.ResponseWriter, r *http.Request) {
	var request VotesRq
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := transactions.New(cfg.pubKey, transactions.Vote, cfg.privKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var votes []storage.Vote
	for _, v := range request.Votes {
		pubKey, err := account.HexToValidatorPubKey(v.PubKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		votes = append(votes, storage.Vote{
			PubKey: pubKey,
			Score:  v.Score,
		})
	}
	b, err := json.Marshal(votes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tx.AddValue(transactions.BytesValue{Value: b})
	err = cfg.client.SendTx(tx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	return
}

func setNebulaHandler(w http.ResponseWriter, r *http.Request) {
	err := setNebula(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	return
}
func setNebula(r *http.Request) error {
	var request SetNebulaRq
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&request)
	if err != nil {
		return err
	}

	tx, err := transactions.New(cfg.pubKey, transactions.SetNebula, cfg.privKey)
	if err != nil {
		return err
	}

	chainType, err := account.ParseChainType(request.ChainType)
	if err != nil {
		return err
	}
	nebulaId, err := account.StringToNebulaId(request.NebulaId, chainType)
	if err != nil {
		return err
	}

	nebulaInfo := storage.NebulaInfo{
		MaxPulseCountInBlock: request.MaxPulseCountInBlock,
		MinScore:             request.MinScore,
		ChainType:            chainType,
		Owner:                cfg.pubKey,
	}
	b, err := json.Marshal(nebulaInfo)
	if err != nil {
		return err
	}

	tx.AddValues([]transactions.Value{
		transactions.BytesValue{Value: nebulaId[:]},
		transactions.BytesValue{Value: b},
	})
	err = cfg.client.SendTx(tx)
	if err != nil {
		return err
	}

	return nil
}
