FROM golang:1.20-alpine AS builder

WORKDIR /app

RUN apk --no-cache add build-base

COPY . .

RUN go build -o monitor

FROM alpine:latest

WORKDIR /app

RUN apk --no-cache add shadow \
    && adduser -u 1000 -D user \
    && chown -R user:user /app \
    && chown -R user:user /var/log \
    && mkdir -p /home/user/.aws

USER user

COPY --from=builder /app/monitor .

EXPOSE 11415

CMD ["./monitor"]
