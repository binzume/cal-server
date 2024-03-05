FROM alpine:3.19
EXPOSE 8080
ADD ./cal-server ./*.ttf /
ENTRYPOINT ["/cal-server"]

