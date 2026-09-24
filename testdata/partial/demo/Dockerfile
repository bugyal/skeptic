FROM alpine:3
RUN apk add --no-cache python3 git patch bash
WORKDIR /testbed
COPY calc.py /testbed/calc.py
COPY runtests.sh /usr/local/bin/runtests.sh
RUN chmod +x /usr/local/bin/runtests.sh \
 && git init -q && git config user.email t@e.com && git config user.name t \
 && git add -A && git commit -qm base
