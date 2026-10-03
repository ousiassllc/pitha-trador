// Package httpbody reads external API response bodies with a size cap, so
// a misconfigured or hostile endpoint cannot make a client buffer an
// unbounded amount of memory.
package httpbody

import (
	"errors"
	"fmt"
	"io"
)

// DefaultMaxBytes is the response size cap for the JSON APIs (Jev,
// kabuステーション). Their legitimate responses are far smaller.
const DefaultMaxBytes = 4 << 20

// ErrTooLarge is returned by ReadAll when the body exceeds the cap.
var ErrTooLarge = errors.New("response body too large")

// ReadAll reads r to EOF like io.ReadAll but fails with ErrTooLarge when
// more than maxBytes bytes are available.
func ReadAll(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w (limit %d bytes)", ErrTooLarge, maxBytes)
	}
	return data, nil
}
