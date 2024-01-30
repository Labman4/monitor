FROM golang:1.20-alpine

WORKDIR /app

RUN apk --no-cache add shadow \
    && useradd -u 1000 user \
    && chown -R user:user /app \
    && mkdir -p /home/user/.aws

USER user

COPY . .

RUN go build -o monitor

EXPOSE 11415

CMD ["./monitor"]