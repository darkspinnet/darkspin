//go:build scenario

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/darkspinnet/darkspin/server/scenario"
)

func TestHeroRetirementWitnessPinsDeleteAndCompleteTraceLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.jsonl")
	const prefix = "{\"protocol\":\"client_raknet\",\"kind\":\"application_receive\",\"message_id\":142,\"frame_sequence\":42,\"payload_hex\":\"8e04000000\"}"
	err := os.WriteFile(path, []byte(prefix), 0600)
	if err != nil {
		t.Fatal(err)
	}
	witness := debugHeroRetirementWitness{owner: debugWorldOwner{client: debugClientState{TracePath: path}}}
	err = witness.observeDelete(4)
	if err != nil {
		t.Fatal(err)
	}
	if witness.isDeleted || witness.offset != 0 {
		t.Fatal("partial trace line manufactured a deletion receipt")
	}
	err = os.WriteFile(path, []byte(prefix+"\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = witness.observeDelete(5)
	if err != nil {
		t.Fatal(err)
	}
	if witness.isDeleted {
		t.Fatal("foreign object deletion satisfied retirement")
	}
	witness.offset = 0
	err = witness.observeDelete(4)
	if err != nil {
		t.Fatal(err)
	}
	if !witness.isDeleted || witness.deletedFrame != 42 || witness.offset != int64(len(prefix)+1) {
		t.Fatal("complete matching deletion did not retain its native frame")
	}
}

func TestHeroReplacementPartialReceiptCannotAssertNativeReadiness(t *testing.T) {
	receipt := scenario.HeroResetReceipt{ObjectID: 4, IsQueued: true, IsReplacement: true}
	dto := debugHeroResetDTO(receipt, false)
	if dto["is_rig_ready"] != false || dto["is_replacement"] != true || dto["is_queued"] != true {
		t.Fatal("partially committed replacement claimed native readiness or lost its accepted phase")
	}
	receipt.IsReplacement, receipt.IsRetirement = false, true
	dto = debugHeroResetDTO(receipt, false)
	if dto["is_retirement"] != true || dto["is_rig_ready"] != false {
		t.Fatal("retirement failure lost its typed partial phase")
	}
}
