# Daka

A habit tracker with a Go backend and a Vue frontend.

## Layout

- `backend/cmd/daka` - program entry point
- `backend/lib/{config,auth,db,dbmodels,holiday,schedule,notify,api}` - backend packages

## Development

Requires Go and [just](https://github.com/casey/just).

- `just test` - run backend tests
- `just lint` - golangci-lint if installed, else `go vet` and a gofmt check
- `just fmt` - format code
- `just run` - run the backend
