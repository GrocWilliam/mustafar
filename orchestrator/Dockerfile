FROM golang:1.26-alpine AS build
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod *.go *.html ./
# Tests run at build time: a failing self-check means no image.
RUN go vet ./... && go test ./... && go build -o /orchestrator .

FROM scratch
COPY --from=build /orchestrator /orchestrator
ENTRYPOINT ["/orchestrator"]
