FROM golang:slim as builder
WORKDIR /app
COPY *.mod .
RUN go mod download
RUN go mod verify
COPY . /app
RUN go build -o /myapp main.go

FROM gcr.io/distroless/base-debian11
COPY --from=builder /myapp /myapp
ENTRYPOINT ["/myapp"]