#!/bin/bash

export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn
go run main.go
exit $?
