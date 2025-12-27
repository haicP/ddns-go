# Build stage
FROM golang:alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o ddns-go .

# Final stage
FROM alpine
LABEL name=ddns-go
LABEL url=https://github.com/jeessy2/ddns-go
RUN apk add --no-cache curl grep

WORKDIR /app
COPY --from=builder /app/ddns-go /app/
# Copy zoneinfo if it exists in the source, otherwise relying on alpine's might need tzdata
RUN apk add --no-cache tzdata
ENV TZ=Asia/Shanghai
EXPOSE 9876
ENTRYPOINT ["/app/ddns-go"]
CMD ["-l", ":9876", "-f", "300"] 