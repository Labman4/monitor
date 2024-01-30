FROM golang:1.20-alpine

WORKDIR /app

COPY . .

RUN go build -o monitor

EXPOSE 11415

CMD ["./monitor"]