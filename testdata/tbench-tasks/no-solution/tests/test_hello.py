from pathlib import Path


# Grades what the agent produced, so an untouched workspace fails. A test that
# passed on its own would make this fixture NOP_PASSES instead of NO_ORACLE.
def test_hello():
    assert Path("/app/hello.txt").read_text().strip() == "Hello, world!"
