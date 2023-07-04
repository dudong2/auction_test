package wasm

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/Finschia/finschia-sdk/types"
	"github.com/Finschia/wasmd/x/wasm/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

var (
	// These come from https://github.com/Finschia/cosmwasm/tree/main/contracts.
	// Hashes of them are in testdata directory.
	calleeContract     = mustLoad("./testdata/dynamic_callee_contract.wasm")
	callerContract     = mustLoad("./testdata/dynamic_caller_contract.wasm")
	numberContract     = mustLoad("./testdata/number.wasm")
	callNumberContract = mustLoad("./testdata/call_number.wasm")

	auctionContract = mustLoad("./testdata/auction.wasm")
	cw721Contract   = mustLoad("./testdata/cw721_base_dynamiclink.wasm")
)

func TestAuctionWorks(t *testing.T) {
	// setup
	data := setupTest(t)

	h := data.module.Route().Handler()
	q := data.module.LegacyQuerierHandler(nil)

	// store cw721 callee code
	storeCalleeMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: cw721Contract,
	}
	res, err := h(data.ctx, storeCalleeMsg)
	require.NoError(t, err)

	calleeCodeId := uint64(1)
	assertStoreCodeResponse(t, res.Data, calleeCodeId)

	// store auction caller code
	storeCallerMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: auctionContract,
	}
	res, err = h(data.ctx, storeCallerMsg)
	require.NoError(t, err)

	callerCodeId := uint64(2)
	assertStoreCodeResponse(t, res.Data, callerCodeId)

	// instantiate cw721 callee contract
	instantiateCalleeMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: calleeCodeId,
		Label:  "callee",
		Msg:    []byte(fmt.Sprintf(`{"name":"cw721","symbol":"cw721","minter":"%s"}`, addr1)),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCalleeMsg)
	require.NoError(t, err)

	calleeContractAddress := parseInitResponse(t, res.Data)

	// instantiate auction caller contract
	instantiateCallerMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: callerCodeId,
		Label:  "caller",
		Msg:    []byte(`{}`),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCallerMsg)
	require.NoError(t, err)

	callerContractAddress := parseInitResponse(t, res.Data)

	// execute mint
	cosmwasmExecuteMsg := fmt.Sprintf(`{"mint":{"token_id":"nft","owner":"%s","token_uri":"uri"}}`, addr1)
	executeMsg := MsgExecuteContract{
		Sender:   addr1,
		Contract: calleeContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsg)
	require.NoError(t, err)

	assert.Equal(t, len(res.Events), 3)
	assert.Equal(t, "wasm", res.Events[2].Type)
	assert.Equal(t, len(res.Events[2].Attributes), 5)
	assertAttribute(t, "action", "mint", res.Events[2].Attributes[1])
	assertAttribute(t, "minter", addr1, res.Events[2].Attributes[2])
	assertAttribute(t, "owner", addr1, res.Events[2].Attributes[3])
	assertAttribute(t, "token_id", "nft", res.Events[2].Attributes[4])

	// execute start_auction
	cosmwasmExecuteMsg = fmt.Sprintf(`{"start_auction":{"expiration_time":1,"cw721_address":"%s","token_id":"nft","start_bid":100}}`, calleeContractAddress)
	executeMsg = MsgExecuteContract{
		Sender:   addr1,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsg)
	require.NoError(t, err)

	assert.Equal(t, len(res.Events), 3)
	assert.Equal(t, "wasm", res.Events[2].Type)
	assert.Equal(t, len(res.Events[2].Attributes), 7)
	assertAttribute(t, "method", "start_auction", res.Events[2].Attributes[1])
	assertAttribute(t, "expiration_time", "1", res.Events[2].Attributes[2])
	assertAttribute(t, "seller", addr1, res.Events[2].Attributes[3])
	assertAttribute(t, "cw721_address", calleeContractAddress, res.Events[2].Attributes[4])
	assertAttribute(t, "token_id", "nft", res.Events[2].Attributes[5])
	assertAttribute(t, "start_bid", "100", res.Events[2].Attributes[6])

	// query get_auction_item
	queryPath := []string{
		QueryGetContractState,
		callerContractAddress,
		keeper.QueryMethodContractStateSmart,
	}
	queryReq := abci.RequestQuery{Data: []byte(`{"get_auction_item":{}}`)}
	qRes, qErr := q(data.ctx, queryPath, queryReq)
	require.NoError(t, qErr)
	assert.Equal(t, []byte(fmt.Sprintf(`{"end_time":"%d","cw721_address":"%s","token_id":"nft","start_bid":100}`, data.ctx.BlockTime().Add(time.Second).UnixNano(), calleeContractAddress)), qRes)

	// execute place_bid
	data.faucet.Fund(data.ctx, sdk.MustAccAddressFromBech32(addr2), sdk.NewCoin("cony", sdk.NewInt(1000)))
	cosmwasmExecuteMsg = `{"place_bid":{"bid":200}}`
	executeMsg = MsgExecuteContract{
		Sender:   addr2,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsg)
	require.NoError(t, err)

	assert.Equal(t, len(res.Events), 3)
	assert.Equal(t, "wasm", res.Events[2].Type)
	assert.Equal(t, len(res.Events[2].Attributes), 4)
	assertAttribute(t, "method", "place_bid", res.Events[2].Attributes[1])
	assertAttribute(t, "bid", "200", res.Events[2].Attributes[2])
	assertAttribute(t, "bidder", addr2, res.Events[2].Attributes[3])

	// query get_highest_bid
	queryPath = []string{
		QueryGetContractState,
		callerContractAddress,
		keeper.QueryMethodContractStateSmart,
	}
	queryReq = abci.RequestQuery{Data: []byte(`{"get_highest_bid":{}}`)}
	qRes, qErr = q(data.ctx, queryPath, queryReq)
	require.NoError(t, qErr)
	assert.Equal(t, []byte(fmt.Sprintf(`{"highest_bid":200,"bidder":"%s"}`, addr2)), qRes)

	// execute end_auction
	endTime := data.ctx.BlockTime().Add(time.Second)
	data.ctx = data.ctx.WithBlockTime(data.ctx.BlockTime().Add(2 * time.Second))
	cosmwasmExecuteMsg = `{"end_auction":{}}`
	executeMsg = MsgExecuteContract{
		Sender:   addr2,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    sdk.NewCoins(sdk.NewCoin("cony", sdk.NewInt(200))),
	}
	res, err = h(data.ctx, &executeMsg)
	require.NoError(t, err)

	assert.Equal(t, len(res.Events), 9)
	assert.Equal(t, "wasm", res.Events[5].Type)
	assert.Equal(t, len(res.Events[5].Attributes), 4)
	assertAttribute(t, "method", "end_auction", res.Events[5].Attributes[1])
	assertAttribute(t, "highest_bid", "200", res.Events[5].Attributes[2])
	assertAttribute(t, "bidder", addr2, res.Events[5].Attributes[3])
	assert.Equal(t, "transfer", res.Events[8].Type)
	assert.Equal(t, len(res.Events[8].Attributes), 3)
	assertAttribute(t, "recipient", addr1, res.Events[8].Attributes[0])
	assertAttribute(t, "sender", callerContractAddress, res.Events[8].Attributes[1])
	assertAttribute(t, "amount", "200cony", res.Events[8].Attributes[2])

	// query get_auction_history
	queryPath = []string{
		QueryGetContractState,
		callerContractAddress,
		keeper.QueryMethodContractStateSmart,
	}
	queryReq = abci.RequestQuery{Data: []byte(`{"get_auction_history":{"idx":0}}`)}
	qRes, qErr = q(data.ctx, queryPath, queryReq)
	require.NoError(t, qErr)
	assert.Equal(t, []byte(fmt.Sprintf(`{"end_time":"%d","seller":"%s","cw721_address":"%s","token_id":"nft","highest_bid":200,"bidder":"%s"}`, endTime.UnixNano(), addr1, calleeContractAddress, addr2)), qRes)

	// check cw721 owner
	queryPath = []string{
		QueryGetContractState,
		calleeContractAddress,
		keeper.QueryMethodContractStateSmart,
	}
	queryReq = abci.RequestQuery{Data: []byte(`{"owner_of":{"token_id":"nft"}}`)}
	qRes, qErr = q(data.ctx, queryPath, queryReq)
	require.NoError(t, qErr)
	assert.Equal(t, []byte(fmt.Sprintf(`{"owner":"%s","approvals":[]}`, addr2)), qRes)
}

// This tests dynamic calls using callee_contract's pong
func TestDynamicPingPongWorks(t *testing.T) {
	// setup
	data := setupTest(t)

	h := data.module.Route().Handler()

	// store dynamic callee code
	storeCalleeMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: calleeContract,
	}
	res, err := h(data.ctx, storeCalleeMsg)
	require.NoError(t, err)

	calleeCodeId := uint64(1)
	assertStoreCodeResponse(t, res.Data, calleeCodeId)

	// store dynamic caller code
	storeCallerMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: callerContract,
	}
	res, err = h(data.ctx, storeCallerMsg)
	require.NoError(t, err)

	callerCodeId := uint64(2)
	assertStoreCodeResponse(t, res.Data, callerCodeId)

	// instantiate callee contract
	instantiateCalleeMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: calleeCodeId,
		Label:  "callee",
		Msg:    []byte(`{}`),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCalleeMsg)
	require.NoError(t, err)

	calleeContractAddress := parseInitResponse(t, res.Data)

	// instantiate caller contract
	cosmwasmInstantiateCallerMsg := fmt.Sprintf(`{"callee_addr":"%s"}`, calleeContractAddress)
	instantiateCallerMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: callerCodeId,
		Label:  "caller",
		Msg:    []byte(cosmwasmInstantiateCallerMsg),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCallerMsg)
	require.NoError(t, err)

	callerContractAddress := parseInitResponse(t, res.Data)

	// execute ping
	cosmwasmExecuteMsg := `{"ping":{"ping_num":"100"}}`
	executeMsg := MsgExecuteContract{
		Sender:   addr1,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsg)
	require.NoError(t, err)

	assert.Equal(t, len(res.Events), 3)
	assert.Equal(t, "wasm", res.Events[2].Type)
	assert.Equal(t, len(res.Events[2].Attributes), 6)
	assertAttribute(t, "returned_pong", "101", res.Events[2].Attributes[1])
	assertAttribute(t, "returned_pong_with_struct", "hello world 101", res.Events[2].Attributes[2])
	assertAttribute(t, "returned_pong_with_tuple", "(hello world, 42)", res.Events[2].Attributes[3])
	assertAttribute(t, "returned_pong_with_tuple_takes_2_args", "(hello world, 42)", res.Events[2].Attributes[4])
	assertAttribute(t, "returned_contract_address", calleeContractAddress, res.Events[2].Attributes[5])
}

// This tests re-entrancy in dynamic call fails
func TestDynamicReEntrancyFails(t *testing.T) {
	// setup
	data := setupTest(t)

	h := data.module.Route().Handler()

	// store dynamic callee code
	storeCalleeMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: calleeContract,
	}
	res, err := h(data.ctx, storeCalleeMsg)
	require.NoError(t, err)

	calleeCodeId := uint64(1)
	assertStoreCodeResponse(t, res.Data, calleeCodeId)

	// store dynamic caller code
	storeCallerMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: callerContract,
	}
	res, err = h(data.ctx, storeCallerMsg)
	require.NoError(t, err)

	callerCodeId := uint64(2)
	assertStoreCodeResponse(t, res.Data, callerCodeId)

	// instantiate callee contract
	instantiateCalleeMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: calleeCodeId,
		Label:  "callee",
		Msg:    []byte(`{}`),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCalleeMsg)
	require.NoError(t, err)

	calleeContractAddress := parseInitResponse(t, res.Data)

	// instantiate caller contract
	cosmwasmInstantiateCallerMsg := fmt.Sprintf(`{"callee_addr":"%s"}`, calleeContractAddress)
	instantiateCallerMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: callerCodeId,
		Label:  "caller",
		Msg:    []byte(cosmwasmInstantiateCallerMsg),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCallerMsg)
	require.NoError(t, err)

	callerContractAddress := parseInitResponse(t, res.Data)

	// execute ping
	cosmwasmExecuteMsg := `{"try_re_entrancy":{}}`
	executeMsg := MsgExecuteContract{
		Sender:   addr1,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsg)
	assert.ErrorContains(t, err, "A contract can only be called once per one call stack.")
}

func TestDynamicLinkInterfaceValidation(t *testing.T) {
	// setup
	data := setupTest(t)

	h := data.module.Route().Handler()

	// store dynamic callee code
	storeCalleeMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: calleeContract,
	}
	res, err := h(data.ctx, storeCalleeMsg)
	require.NoError(t, err)

	calleeCodeId := uint64(1)
	assertStoreCodeResponse(t, res.Data, calleeCodeId)

	// store dynamic caller code
	storeCallerMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: callerContract,
	}
	res, err = h(data.ctx, storeCallerMsg)
	require.NoError(t, err)

	callerCodeId := uint64(2)
	assertStoreCodeResponse(t, res.Data, callerCodeId)

	// instantiate callee contract
	instantiateCalleeMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: calleeCodeId,
		Label:  "callee",
		Msg:    []byte(`{}`),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCalleeMsg)
	require.NoError(t, err)

	calleeContractAddress := parseInitResponse(t, res.Data)

	// instantiate caller contract
	cosmwasmInstantiateCallerMsg := fmt.Sprintf(`{"callee_addr":"%s"}`, calleeContractAddress)
	instantiateCallerMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: callerCodeId,
		Label:  "caller",
		Msg:    []byte(cosmwasmInstantiateCallerMsg),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCallerMsg)
	require.NoError(t, err)

	callerContractAddress := parseInitResponse(t, res.Data)

	// execute validate interface
	cosmwasmExecuteMsg := `{"validate_interface":{}}`
	executeMsg := MsgExecuteContract{
		Sender:   addr1,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsg)
	require.NoError(t, err)

	// execute validate interface error
	cosmwasmExecuteMsgErr := `{"validate_interface_err":{}}`
	executeMsgErr := MsgExecuteContract{
		Sender:   addr1,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsgErr),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsgErr)
	assert.ErrorContains(t, err, "The following functions are not implemented:")
}

// This tests both of dynamic calls and traditional queries can be used
// in a contract call
func TestDynamicCallAndTraditionalQueryWork(t *testing.T) {
	// setup
	data := setupTest(t)

	h := data.module.Route().Handler()
	q := data.module.LegacyQuerierHandler(nil)

	// store callee code (number)
	storeCalleeMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: numberContract,
	}
	res, err := h(data.ctx, storeCalleeMsg)
	require.NoError(t, err)

	calleeCodeId := uint64(1)
	assertStoreCodeResponse(t, res.Data, calleeCodeId)

	// store caller code (call-number)
	storeCallerMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: callNumberContract,
	}
	res, err = h(data.ctx, storeCallerMsg)
	require.NoError(t, err)

	callerCodeId := uint64(2)
	assertStoreCodeResponse(t, res.Data, callerCodeId)

	// instantiate callee contract
	instantiateCalleeMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: calleeCodeId,
		Label:  "number",
		Msg:    []byte(`{"value":21}`),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCalleeMsg)
	require.NoError(t, err)

	calleeContractAddress := parseInitResponse(t, res.Data)

	// instantiate caller contract
	cosmwasmInstantiateCallerMsg := fmt.Sprintf(`{"callee_addr":"%s"}`, calleeContractAddress)
	instantiateCallerMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: callerCodeId,
		Label:  "call-number",
		Msg:    []byte(cosmwasmInstantiateCallerMsg),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCallerMsg)
	require.NoError(t, err)

	callerContractAddress := parseInitResponse(t, res.Data)

	// traditional queries from caller
	queryPath := []string{
		QueryGetContractState,
		callerContractAddress,
		keeper.QueryMethodContractStateSmart,
	}
	queryReq := abci.RequestQuery{Data: []byte(`{"number":{}}`)}
	qRes, qErr := q(data.ctx, queryPath, queryReq)
	require.NoError(t, qErr)
	assert.Equal(t, []byte(`{"value":21}`), qRes)

	// query via dynamic call from caller
	dynQueryReq := abci.RequestQuery{Data: []byte(`{"number_dyn":{}}`)}
	qRes, qErr = q(data.ctx, queryPath, dynQueryReq)
	require.NoError(t, qErr)
	assert.Equal(t, []byte(`{"value":21}`), qRes)

	// execute mul
	cosmwasmExecuteMsg := `{"mul":{"value":2}}`
	executeMsg := MsgExecuteContract{
		Sender:   addr1,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsg)
	require.NoError(t, err)
	assert.Equal(t, len(res.Events), 3)
	assert.Equal(t, "wasm", res.Events[2].Type)
	assert.Equal(t, len(res.Events[2].Attributes), 3)
	assertAttribute(t, "value_by_dynamic", "42", res.Events[2].Attributes[1])
	assertAttribute(t, "value_by_query", "42", res.Events[2].Attributes[2])

	// queries
	qRes, qErr = q(data.ctx, queryPath, queryReq)
	require.NoError(t, qErr)
	assert.Equal(t, []byte(`{"value":42}`), qRes)
	qRes, qErr = q(data.ctx, queryPath, dynQueryReq)
	require.NoError(t, qErr)
	assert.Equal(t, []byte(`{"value":42}`), qRes)
}

// This tests dynamic call with writing something to storage fails
// if it is called by a query
func TestDynamicCallWithWriteFailsByQuery(t *testing.T) {
	// setup
	data := setupTest(t)

	h := data.module.Route().Handler()
	q := data.module.LegacyQuerierHandler(nil)

	// store callee code (number)
	storeCalleeMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: numberContract,
	}
	res, err := h(data.ctx, storeCalleeMsg)
	require.NoError(t, err)

	calleeCodeId := uint64(1)
	assertStoreCodeResponse(t, res.Data, calleeCodeId)

	// store caller code (call-number)
	storeCallerMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: callNumberContract,
	}
	res, err = h(data.ctx, storeCallerMsg)
	require.NoError(t, err)

	callerCodeId := uint64(2)
	assertStoreCodeResponse(t, res.Data, callerCodeId)

	// instantiate callee contract
	instantiateCalleeMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: calleeCodeId,
		Label:  "number",
		Msg:    []byte(`{"value":21}`),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCalleeMsg)
	require.NoError(t, err)

	calleeContractAddress := parseInitResponse(t, res.Data)

	// instantiate caller contract
	cosmwasmInstantiateCallerMsg := fmt.Sprintf(`{"callee_addr":"%s"}`, calleeContractAddress)
	instantiateCallerMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: callerCodeId,
		Label:  "call-number",
		Msg:    []byte(cosmwasmInstantiateCallerMsg),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCallerMsg)
	require.NoError(t, err)

	callerContractAddress := parseInitResponse(t, res.Data)

	// query which tries to write value to storage
	queryPath := []string{
		QueryGetContractState,
		callerContractAddress,
		keeper.QueryMethodContractStateSmart,
	}
	queryReq := abci.RequestQuery{Data: []byte(`{"mul":{"value":2}}`)}
	_, qErr := q(data.ctx, queryPath, queryReq)
	assert.ErrorContains(t, qErr, "a read-write callable point is called in read-only context")
}

// This tests callee_panic in dynamic call fails
func TestDynamicCallCalleeFails(t *testing.T) {
	// setup
	data := setupTest(t)

	h := data.module.Route().Handler()

	// store dynamic callee code
	storeCalleeMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: calleeContract,
	}
	res, err := h(data.ctx, storeCalleeMsg)
	require.NoError(t, err)

	calleeCodeId := uint64(1)
	assertStoreCodeResponse(t, res.Data, calleeCodeId)

	// store dynamic caller code
	storeCallerMsg := &MsgStoreCode{
		Sender:       addr1,
		WASMByteCode: callerContract,
	}
	res, err = h(data.ctx, storeCallerMsg)
	require.NoError(t, err)

	callerCodeId := uint64(2)
	assertStoreCodeResponse(t, res.Data, callerCodeId)

	// instantiate callee contract
	instantiateCalleeMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: calleeCodeId,
		Label:  "callee",
		Msg:    []byte(`{}`),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCalleeMsg)
	require.NoError(t, err)

	calleeContractAddress := parseInitResponse(t, res.Data)

	// instantiate caller contract
	cosmwasmInstantiateCallerMsg := fmt.Sprintf(`{"callee_addr":"%s"}`, calleeContractAddress)
	instantiateCallerMsg := &MsgInstantiateContract{
		Sender: addr1,
		CodeID: callerCodeId,
		Label:  "caller",
		Msg:    []byte(cosmwasmInstantiateCallerMsg),
		Funds:  nil,
	}
	res, err = h(data.ctx, instantiateCallerMsg)
	require.NoError(t, err)

	callerContractAddress := parseInitResponse(t, res.Data)

	// execute do_panic
	cosmwasmExecuteMsg := `{"do_panic":{}}`
	executeMsg := MsgExecuteContract{
		Sender:   addr1,
		Contract: callerContractAddress,
		Msg:      []byte(cosmwasmExecuteMsg),
		Funds:    nil,
	}
	res, err = h(data.ctx, &executeMsg)
	assert.ErrorContains(t, err, "Error in dynamic link")
	assert.ErrorContains(t, err, "RuntimeError: unreachable")
}
