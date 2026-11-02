package minedeck.companion;

import com.google.gson.JsonObject;

/** 指令执行失败：回 RESPONSE {"ok":false,"code":…,"error":…}，extra 里的字段一起带上。 */
final class OpError extends RuntimeException {
    private static final long serialVersionUID = 1L;

    final String code;
    final JsonObject extra = new JsonObject();

    OpError(String code, String message) {
        super(message);
        this.code = code;
    }

    OpError with(String key, Number value) {
        extra.addProperty(key, value);
        return this;
    }

    static OpError badArgs(String message) {
        return new OpError("bad_args", message);
    }
}
