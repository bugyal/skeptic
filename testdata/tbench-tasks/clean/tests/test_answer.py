def test_answer():
    assert open("/app/answer.txt").read().strip() == "42"
