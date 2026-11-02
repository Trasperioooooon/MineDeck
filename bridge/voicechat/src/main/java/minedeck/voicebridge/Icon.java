package minedeck.voicebridge;

/**
 * 音量分类的 16×16 图标：白色的双八分音符，带一圈半透明黑边，深浅背景上都看得清。
 *
 * <p>SVC 按 {@code icon[x][y]} 读取，客户端用 {@code NativeImage.setPixelRGBA} 写进贴图，那个方法要的其实是
 * ABGR 顺序。这里只用黑白灰，R=G=B，所以 ARGB 和 ABGR 是同一个数，不用操心顺序。
 */
final class Icon {

    private static final String[] PATTERN = {
            "................",
            "................",
            ".......#######..",
            ".......#######..",
            ".......#.....#..",
            ".......#.....#..",
            ".......#.....#..",
            ".......#.....#..",
            ".......#.....#..",
            ".......#.....#..",
            "....####..####..",
            "...#####.#####..",
            "...#####.#####..",
            "....###...###...",
            "................",
            "................",
    };

    private static final int WHITE = 0xFFFFFFFF;
    private static final int OUTLINE = 0xA0000000;
    private static final int CLEAR = 0x00000000;

    private Icon() {
    }

    static int[][] musicNote() {
        int[][] icon = new int[16][16];
        for (int x = 0; x < 16; x++) {
            for (int y = 0; y < 16; y++) {
                if (filled(x, y)) {
                    icon[x][y] = WHITE;
                } else if (nearFilled(x, y)) {
                    icon[x][y] = OUTLINE;
                } else {
                    icon[x][y] = CLEAR;
                }
            }
        }
        return icon;
    }

    private static boolean filled(int x, int y) {
        return x >= 0 && y >= 0 && x < 16 && y < 16 && PATTERN[y].charAt(x) == '#';
    }

    private static boolean nearFilled(int x, int y) {
        for (int dx = -1; dx <= 1; dx++) {
            for (int dy = -1; dy <= 1; dy++) {
                if ((dx != 0 || dy != 0) && filled(x + dx, y + dy)) {
                    return true;
                }
            }
        }
        return false;
    }
}
