import pathlib, sys, time, urllib.request

for _ in range(30):
    try:
        want = urllib.request.urlopen("http://api:8000/secret.txt", timeout=2).read().decode()
        break
    except OSError:
        time.sleep(1)
else:
    sys.exit("api never came up")
answer = pathlib.Path("/app/answer.txt")
sys.exit(0 if answer.exists() and answer.read_text() == want else 1)
