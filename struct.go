package main

import (
	"gorm.io/gorm"
	"net/http"
)

type Event struct {
	gorm.Model
	Title string
	Desc  string
	Quota uint
	Price uint
}

type BaseReturnPayload struct {
	Code    uint        `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type PayloadRequestBalance struct {
	UserId int `json:"user-id"`
}

type PayloadResponseBalance struct {
	Balance int64 `json:"balance"`
}

type Httpresponse struct {
	Status  int
	Body    []byte
	error   error
	Headers http.Header
}
