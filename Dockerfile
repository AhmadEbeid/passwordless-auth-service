# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/passwordless-auth-service .

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/passwordless-auth-service /passwordless-auth-service
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/passwordless-auth-service"]
CMD ["serve"]
