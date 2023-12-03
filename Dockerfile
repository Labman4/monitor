FROM golang:1.17-alpine

WORKDIR /app

COPY . .

RUN go build -o ipquery

EXPOSE 114115

# 运行应用程序
CMD ["./ipquery"]