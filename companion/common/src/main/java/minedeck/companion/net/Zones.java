package minedeck.companion.net;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * 禁挖区域（配置文件里的 nodig.&lt;名字&gt;）。对象建好后不再改，可以在任意线程上读：
 * 配置变了就整个换一个新的（{@link CompanionConfig}）。
 */
public final class Zones {

    public static final Zones NONE = new Zones(Collections.<Zone>emptyList());

    /** 一个长方体区域，两端都包含。 */
    public static final class Zone {
        public final String name;
        public final String dim;
        public final int minX;
        public final int minY;
        public final int minZ;
        public final int maxX;
        public final int maxY;
        public final int maxZ;

        public Zone(String name, String dim, int x1, int y1, int z1, int x2, int y2, int z2) {
            this.name = name;
            this.dim = dim;
            this.minX = Math.min(x1, x2);
            this.minY = Math.min(y1, y2);
            this.minZ = Math.min(z1, z2);
            this.maxX = Math.max(x1, x2);
            this.maxY = Math.max(y1, y2);
            this.maxZ = Math.max(z1, z2);
        }

        public boolean contains(String dim, int x, int y, int z) {
            return this.dim.equals(dim) && x >= minX && x <= maxX && y >= minY && y <= maxY && z >= minZ && z <= maxZ;
        }

        /** 点 (x, y, z) 到这个区域（方块的外边界）的距离的平方；在区域里面是 0。 */
        public double distanceSq(double x, double y, double z) {
            double dx = x < minX ? minX - x : (x > maxX + 1 ? x - maxX - 1 : 0);
            double dy = y < minY ? minY - y : (y > maxY + 1 ? y - maxY - 1 : 0);
            double dz = z < minZ ? minZ - z : (z > maxZ + 1 ? z - maxZ - 1 : 0);
            return dx * dx + dy * dy + dz * dz;
        }

        @Override
        public String toString() {
            return name + "（" + dim + " " + minX + " " + minY + " " + minZ + " → " + maxX + " " + maxY + " " + maxZ + "）";
        }
    }

    private final List<Zone> zones;

    public Zones(List<Zone> zones) {
        this.zones = Collections.unmodifiableList(new ArrayList<Zone>(zones));
    }

    public List<Zone> all() {
        return zones;
    }

    public boolean isEmpty() {
        return zones.isEmpty();
    }

    /** 包含这个方块的第一个区域；不在任何区域里时为 null。 */
    public Zone find(String dim, int x, int y, int z) {
        for (Zone zone : zones) {
            if (zone.contains(dim, x, y, z)) {
                return zone;
            }
        }
        return null;
    }

    /** 这个维度里离 (x, y, z) 最近的区域；这个维度没有区域时为 null。 */
    public Zone nearest(String dim, double x, double y, double z) {
        Zone best = null;
        double bestD = Double.MAX_VALUE;
        for (Zone zone : zones) {
            if (!zone.dim.equals(dim)) {
                continue;
            }
            double d = zone.distanceSq(x, y, z);
            if (d < bestD) {
                bestD = d;
                best = zone;
            }
        }
        return best;
    }

    /** (x, y, z) 到这个维度里最近的区域的距离的平方；这个维度没有区域时是 Double.MAX_VALUE。 */
    public double distanceSq(String dim, double x, double y, double z) {
        Zone zone = nearest(dim, x, y, z);
        return zone == null ? Double.MAX_VALUE : zone.distanceSq(x, y, z);
    }
}
