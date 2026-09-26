# alpine/git ships git, so the build needs no package index and works offline.
FROM alpine/git:v2.49.1
WORKDIR /src
COPY calc.sh /src/calc.sh
RUN git init -q && git add calc.sh && git -c user.email=f@x -c user.name=f commit -qm base
