# Holds 300 MB, well past the 64 MB the task declares, then grades honestly.
ballast = bytearray(300 * 1024 * 1024)
with open("/app/answer.txt") as f:
    assert f.read().strip() == "42"
print("ok")
