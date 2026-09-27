#!/bin/sh
python3 -c "import urllib.request; open('/app/answer.txt','w').write(urllib.request.urlopen('http://api:8000/secret.txt').read().decode())"
