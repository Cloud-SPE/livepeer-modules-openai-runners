import unittest

from .gpu_probe import _arch_supported

CU128 = ["sm_75", "sm_80", "sm_86", "sm_90", "sm_100", "sm_120"]
CU126 = ["sm_50", "sm_60", "sm_70", "sm_75", "sm_80", "sm_86", "sm_90"]


class ArchSupportTest(unittest.TestCase):
    def test_pascal_is_not_in_cu128_wheels(self):
        self.assertFalse(_arch_supported(CU128, 6, 1))  # GTX 1080

    def test_pascal_runs_on_cu126_sm60(self):
        self.assertTrue(_arch_supported(CU126, 6, 1))

    def test_turing_and_newer(self):
        self.assertTrue(_arch_supported(CU128, 7, 5))  # GTX 1650
        self.assertTrue(_arch_supported(CU128, 8, 9))  # RTX 4090 via sm_86
        self.assertTrue(_arch_supported(CU128, 12, 0))

    def test_ptx_fallback_and_garbage(self):
        self.assertTrue(_arch_supported(["compute_60"], 6, 1))
        self.assertFalse(_arch_supported(["compute_75"], 6, 1))
        self.assertFalse(_arch_supported(["weird", "sm_x"], 7, 5))


if __name__ == "__main__":
    unittest.main()
