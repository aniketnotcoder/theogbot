package forward

import (
	"testing"
	"github.com/gotd/td/tg"
)

func TestExtractMessageIDsSkipsServiceEvents(t *testing.T) {
	result := &tg.MessagesMessages{Messages: []tg.MessageClass{
		&tg.MessageService{ID: 10}, &tg.Message{ID: 11},
		&tg.MessageService{ID: 12}, &tg.Message{ID: 13},
	}}
	got, err := extractMessageIDs(result)
	if err != nil { t.Fatal(err) }
	if len(got) != 2 || got[0] != 11 || got[1] != 13 { t.Fatalf("history IDs = %v, want [11 13]", got) }
}
