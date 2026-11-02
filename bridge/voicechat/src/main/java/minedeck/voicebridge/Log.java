package minedeck.voicebridge;

import java.lang.reflect.Method;
import java.text.SimpleDateFormat;
import java.util.Date;

/**
 * 日志。
 *
 * <p>Forge 1.20.1 和 NeoForge 1.21.1 的服务端都带 SLF4J，这里用反射拿它，日志就会和 SVC 的一起进
 * {@code logs/latest.log}。拿不到时（比如在 devtest 里）打印到标准输出。编译期不依赖任何日志库。
 */
final class Log {

    private static final String NAME = "minedeck_voicebridge";
    private static final Object LOGGER;
    private static final Method INFO;
    private static final Method WARN;
    private static final Method ERROR;
    private static final Method ERROR_WITH_CAUSE;

    static {
        Object logger = null;
        Method info = null;
        Method warn = null;
        Method error = null;
        Method errorWithCause = null;
        if (!Boolean.getBoolean("minedeck.voicebridge.stdoutLog")) {
            try {
                Class<?> api = Class.forName("org.slf4j.Logger");
                Class<?> factory = Class.forName("org.slf4j.LoggerFactory");
                logger = factory.getMethod("getLogger", String.class).invoke(null, NAME);
                info = api.getMethod("info", String.class);
                warn = api.getMethod("warn", String.class);
                error = api.getMethod("error", String.class);
                errorWithCause = api.getMethod("error", String.class, Throwable.class);
            } catch (Throwable t) {
                logger = null;
            }
        }
        LOGGER = logger;
        INFO = info;
        WARN = warn;
        ERROR = error;
        ERROR_WITH_CAUSE = errorWithCause;
    }

    private Log() {
    }

    static void info(String message) {
        if (!call(INFO, message, null)) {
            print("INFO", message, null);
        }
    }

    static void warn(String message) {
        if (!call(WARN, message, null)) {
            print("WARN", message, null);
        }
    }

    static void error(String message, Throwable cause) {
        boolean done = cause == null ? call(ERROR, message, null) : call(ERROR_WITH_CAUSE, message, cause);
        if (!done) {
            print("ERROR", message, cause);
        }
    }

    private static boolean call(Method method, String message, Throwable cause) {
        if (LOGGER == null || method == null) {
            return false;
        }
        try {
            if (cause == null) {
                method.invoke(LOGGER, message);
            } else {
                method.invoke(LOGGER, message, cause);
            }
            return true;
        } catch (Throwable t) {
            return false;
        }
    }

    private static synchronized void print(String level, String message, Throwable cause) {
        String time = new SimpleDateFormat("HH:mm:ss.SSS").format(new Date());
        System.out.println("[" + time + "] [" + level + "] [" + NAME + "] " + message);
        if (cause != null) {
            cause.printStackTrace(System.out);
        }
        System.out.flush();
    }
}
