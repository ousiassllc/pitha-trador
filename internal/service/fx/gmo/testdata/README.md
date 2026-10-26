# fixtures

These JSON files are **hand-built from the schema documented at
<https://api.coin.z.com/fxdocs/>** (ticker, klines, the error-code table). They
are not recordings of live responses; the prices are made up. The error
fixtures use the `{"status": N, "messages": [{"message_code", "message_string"}]}`
shape of GMO's API family. The official FX page documents the error codes but
shows no example error response, so that envelope (and the HTTP status and
`status` value used for maintenance) is not confirmed for the FX API.

Tests never call the real API; they serve these files from `httptest`.
