package raknet

import (
	"bytes"
	"testing"
)

func TestObjectPresentationRetainsOnlyAuthoredFields(t *testing.T) {
	packet, err := MarshalApplication(ObjectPresentationMessage{Asset: 0x12345678, ObjectID: 4})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := []byte{byte(ServerEvent), 6, 0x78, 0x56, 0x34, 0x12, 7, 4, 0, 0, 0, 0xff}
	if !bytes.Equal(packet, want) {
		t.Fatalf("object presentation: got %x want %x", packet, want)
	}
}
