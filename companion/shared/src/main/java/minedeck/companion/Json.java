package minedeck.companion;

import java.util.List;
import java.util.Map;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonNull;
import com.google.gson.JsonObject;
import com.google.gson.JsonPrimitive;

/** 拼 JSON 的小工具（Gson 是 Minecraft 自带的）。null 也写出来（"task":null），MineDeck 那边好判断。 */
final class Json {

    static final Gson GSON = new GsonBuilder().serializeNulls().disableHtmlEscaping().create();

    private Json() {
    }

    static String str(JsonElement e) {
        return GSON.toJson(e);
    }

    static JsonObject event(String name) {
        JsonObject o = new JsonObject();
        o.addProperty("event", name);
        return o;
    }

    static JsonElement orNull(String s) {
        return s == null ? JsonNull.INSTANCE : new JsonPrimitive(s);
    }

    static double round(double v) {
        return Math.round(v * 10.0) / 10.0;
    }

    /** 把 CompanionConfig.summary() 这类 Map/List/数组/基本类型转成 JSON。 */
    static JsonElement fromJava(Object v) {
        if (v == null) {
            return JsonNull.INSTANCE;
        }
        if (v instanceof JsonElement) {
            return (JsonElement) v;
        }
        if (v instanceof String) {
            return new JsonPrimitive((String) v);
        }
        if (v instanceof Number) {
            return new JsonPrimitive((Number) v);
        }
        if (v instanceof Boolean) {
            return new JsonPrimitive((Boolean) v);
        }
        if (v instanceof int[]) {
            JsonArray a = new JsonArray();
            for (int x : (int[]) v) {
                a.add(x);
            }
            return a;
        }
        if (v instanceof List) {
            JsonArray a = new JsonArray();
            for (Object x : (List<?>) v) {
                a.add(fromJava(x));
            }
            return a;
        }
        if (v instanceof Map) {
            JsonObject o = new JsonObject();
            for (Map.Entry<?, ?> e : ((Map<?, ?>) v).entrySet()) {
                o.add(String.valueOf(e.getKey()), fromJava(e.getValue()));
            }
            return o;
        }
        return new JsonPrimitive(String.valueOf(v));
    }
}
