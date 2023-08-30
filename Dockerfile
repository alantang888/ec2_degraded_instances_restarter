FROM golang:1.20-bullseye as build

WORKDIR /go/src/github.com/alantang888/ec2_degraded_instances_restarter
COPY . .
RUN go mod download
WORKDIR /go/src/github.com/alantang888/ec2_degraded_instances_restarter
RUN go build -o /go/bin/app

FROM gcr.io/distroless/base
COPY --from=build /go/bin/app /

CMD ["/app"]
