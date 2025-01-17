# scv-go-tools v3
![CI](https://github.com/tehuelche/scv-go-tools/actions/workflows/ci.yml/badge.svg)
![Coverage](https://img.shields.io/badge/Coverage-100.0%25-brightgreen)
[![Go Reference](https://pkg.go.dev/badge/github.com/tehuelche/scv-go-tools/v3.svg)](https://pkg.go.dev/github.com/tehuelche/scv-go-tools/v3)

Tools for building REST APIs in Go.

## Included packages
- api/middlewares: provides Middlewares for panic recovering and JWT authentication & role-based authorization.
- api/utils: provides JSON success & error responses with http status codes management and a function for unmarshalling JSON files into structs.
- infrastructure: provides MongoDB and PostgreSQL connection functions, a migration runner for PostgresDB and a generic implemention of the Repository interface for MongoDB.
- mocks: provides mock creation functions for MongoDB and PostgreSQL.
- repository: provides an interface of the repository pattern with CRUD operations.
- wrappers: provides convenient wraps for having custom types.
- testutils: provides convenient testing helper functions.

## Usage steps
1. Create an empty repository and clone it.
2. Execute:
```
go mod init github.com/{username}/{repository_name}
go get github.com/tehuelche/scv-go-tools/v3
```

## Run all unit tests with code coverage
```
make test-unit
```

## View coverage report
```
make cover
```

## Usage examples
[go-hexagonal-restapi](https://github.com/tehuelche/go-hexagonal-api)

## Author
Sergi Canet Vela

## License
This project is licensed under the terms of the MIT license.
