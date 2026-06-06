FROM gcr.io/distroless/static-debian13:latest

WORKDIR /data

COPY moyu /moyu
COPY index.html .

ENTRYPOINT ["/moyu"]
