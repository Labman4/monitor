FROM golang:1.17-alpine

WORKDIR /app

COPY . .

RUN go build -o ipquery

EXPOSE 11415

CMD ["./ipquery"]