FROM golang:1.18-alpine

WORKDIR /
COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY main.go ./

RUN chmod +rwx ./tmp

EXPOSE 8080

RUN go build -o go-module-server main.go

CMD [ "/go-module-server" ]
