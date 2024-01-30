FROM golang:1.20-alpine

WORKDIR /app

RUN apk --no-cache add shadow \
    && useradd -u 1000 -m user \
    && chown -R user:user /app \
    && chown -R user:user /var/log

USER user

RUN mkdir -p /home/user/.aws /home/user/.cache

COPY . .

RUN go build -o monitor

EXPOSE 11415

CMD ["./monitor"]