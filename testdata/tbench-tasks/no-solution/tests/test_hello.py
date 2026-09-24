import subprocess

def test_hello():
    out = subprocess.run(["echo", "hello"], capture_output=True).stdout
    assert out.strip() == b"hello"
