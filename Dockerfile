FROM golang:1.9

WORKDIR /go/src/app
COPY ec2_degraded_instances_restarter.go .

RUN go get -d -v ./...
RUN go install -v ./...

CMD ["app"]