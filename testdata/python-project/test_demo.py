"""A small deterministic pytest workload for Receipt's smoke test."""


def test_addition():
    assert 2 + 3 == 5


def test_sorted():
    assert sorted([3, 1, 2]) == [1, 2, 3]
