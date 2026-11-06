package forward

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/gotd/td/tg"
)

const MaxIDsPerRequest = 100

var ErrInvalidMessageIDs = errors.New("message IDs must be positive")

type Result struct {
	Requested int
	ForwardedBatches int
}

// IDsInRange returns inclusive, ascending message IDs. A hard cap prevents
// accidental requests that would create unbounded work.
func IDsInRange(first, last, max int) ([]int, error) {
	if first <= 0 || last <= 0 || last < first {
		return nil, ErrInvalidMessageIDs
	}
	if max <= 0 {
		max = 5000
	}
	if last-first+1 > max {
		return nil, fmt.Errorf("range contains more than %d messages", max)
	}
	ids := make([]int, 0, last-first+1)
	for id := first; id <= last; id++ {
		ids = append(ids, id)
	}
	return ids, nil
}

// ForwardIDs uses Telegram's native messages.forwardMessages RPC. It does not
// download or re-upload media. Telegram's own forwarding and protected-content
// restrictions remain authoritative; RPC errors are returned to the caller.
func ForwardIDs(ctx context.Context, api *tg.Client, from, to tg.InputPeerClass, ids []int) (Result, error) {
	if api == nil || from == nil || to == nil {
		return Result{}, errors.New("MTProto API and source/destination peers are required")
	}
	if len(ids) == 0 {
		return Result{}, errors.New("no message IDs supplied")
	}
	for _, id := range ids {
		if id <= 0 {
			return Result{}, ErrInvalidMessageIDs
		}
	}

	result := Result{Requested: len(ids)}
	for start := 0; start < len(ids); start += MaxIDsPerRequest {
		end := start + MaxIDsPerRequest
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		randomIDs, err := randomIDs(len(batch))
		if err != nil {
			return result, fmt.Errorf("generate forwarding IDs: %w", err)
		}
		_, err = api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
			FromPeer: from,
			ToPeer: to,
			ID: append([]int(nil), batch...),
			RandomID: randomIDs,
		})
		if err != nil {
			// Do not attempt an alternate transfer method when Telegram rejects
			// forwarding (including protected-content restrictions).
			return result, fmt.Errorf("forward messages %d-%d: %w", batch[0], batch[len(batch)-1], err)
		}
		result.ForwardedBatches++
	}
	return result, nil
}

func randomIDs(n int) ([]int64, error) {
	out := make([]int64, n)
	var raw [8]byte
	for i := range out {
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, err
		}
		out[i] = int64(binary.LittleEndian.Uint64(raw[:]) & 0x7fffffffffffffff)
		if out[i] == 0 {
			out[i] = int64(i + 1)
		}
	}
	return out, nil
}
