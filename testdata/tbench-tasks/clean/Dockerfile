FROM python:3.11-slim
# Terminal-Bench images ship the test runner; run-tests.sh calls pytest.
RUN pip install --no-cache-dir pytest==8.3.3
COPY answer.txt /app/answer.txt
