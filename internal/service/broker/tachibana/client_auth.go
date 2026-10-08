package tachibana

import (
	"context"
	"crypto/rsa"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
)

// LoginInfo is what a successful login reports besides the virtual URLs
// (which stay inside the Client): the broker's notice dates, YYYYMMDD or "".
type LoginInfo struct {
	// APISpecUpdate is sUpdateInformAPISpecFunction (API リリース予定日).
	APISpecUpdate string
	// DocumentUpdate is sUpdateInformWebDocument (交付書面更新予定日).
	DocumentUpdate string
}

// loginAck is the CLMAuthLoginAck payload the adapter reads. The Url*
// fields are the RSA-OAEP encrypted virtual URLs.
type loginAck struct {
	DocumentsUnread flexInt `json:"sKinsyouhouMidokuFlg"`
	URLRequest      string  `json:"sUrlRequest"`
	URLMaster       string  `json:"sUrlMaster"`
	URLPrice        string  `json:"sUrlPrice"`
	URLEvent        string  `json:"sUrlEvent"`
	URLEventWS      string  `json:"sUrlEventWebSocket"`
	DocumentUpdate  string  `json:"sUpdateInformWebDocument"`
	APISpecUpdate   string  `json:"sUpdateInformAPISpecFunction"`
}

// Login authenticates authID (HTTPS POST to {base}/auth/), decrypts the
// virtual URLs with key and installs them as the Client's session, valid
// until the next 03:30 close. Like every request it waits its turn in the
// queue, ahead of all of them. A successful login restarts p_no at 1 and, as
// the broker allows one virtual URL per customer, invalidates the previous
// session.
//
// Errors: *APIError (control error such as -62, or sResultCode such as
// 10031), ErrDocumentsUnread (accepted, no URLs; the returned LoginInfo is
// still filled), ErrDecryptURL, *HTTPStatusError or a transport error.
func (c *Client) Login(ctx context.Context, authID string, key *rsa.PrivateKey) (LoginInfo, error) {
	release, err := c.gate.acquire(ctx, PrioritySession)
	if err != nil {
		return LoginInfo{}, err
	}
	defer release()

	previous := c.pNo
	c.pNo = 0
	var ack loginAck
	err = c.send(ctx, c.baseURL+"auth/", "CLMAuthLoginRequest", map[string]string{"sAuthId": authID}, &ack, httpbody.DefaultMaxBytes)
	if err != nil {
		c.pNo = previous
		return LoginInfo{}, err
	}
	info := LoginInfo{APISpecUpdate: ack.APISpecUpdate, DocumentUpdate: ack.DocumentUpdate}
	if ack.DocumentsUnread == 1 {
		c.pNo = previous
		return info, ErrDocumentsUnread
	}
	urls, err := decryptURLs(key, ack)
	if err != nil {
		c.pNo = previous
		return info, err
	}
	c.setSession(urls, NextClose(c.clock.Now()))
	return info, nil
}

func decryptURLs(key *rsa.PrivateKey, ack loginAck) (virtualURLs, error) {
	var urls virtualURLs
	for _, f := range []struct {
		dst *string
		src string
	}{
		{&urls.request, ack.URLRequest},
		{&urls.master, ack.URLMaster},
		{&urls.price, ack.URLPrice},
		{&urls.event, ack.URLEvent},
		{&urls.eventWebSocket, ack.URLEventWS},
	} {
		v, err := decryptVirtualURL(key, f.src)
		if err != nil {
			return virtualURLs{}, err
		}
		*f.dst = v
	}
	return urls, nil
}

// Logout ends the session (CLMAuthLogoutRequest on the REQUEST URL) so no
// virtual URL outlives the process, and forgets it. It is a no-op without a
// session; a session the broker already dropped counts as logged out.
func (c *Client) Logout(ctx context.Context) error {
	_, gen, ok := c.session()
	if !ok {
		return nil
	}
	err := c.Call(ctx, TargetRequest, PrioritySession, "CLMAuthLogoutRequest", nil, nil)
	c.dropSession(gen)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Kind() == KindSessionExpired {
		return nil
	}
	return err
}
