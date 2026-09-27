FROM python:3.12-slim
WORKDIR /srv
RUN echo "hunter2-$(date +%s)" > /srv/secret.txt
CMD ["python", "-m", "http.server", "8000"]
