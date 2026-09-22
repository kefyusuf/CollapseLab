FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/collapselab-sut ./sut/api/cmd/server

FROM scratch
COPY --from=build /out/collapselab-sut /collapselab-sut
ENTRYPOINT ["/collapselab-sut"]
