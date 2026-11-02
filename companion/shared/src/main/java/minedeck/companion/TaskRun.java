package minedeck.companion;

import com.google.gson.JsonObject;

/** MineDeck 交给伙伴的一个任务（collect / follow / goto / attack / give）。只在服务器主线程上读写。 */
final class TaskRun {
    final long id;
    final int companion;
    final String op;
    /** 整理过的参数（补上了默认值），原样放进任务对象的 args。 */
    final JsonObject args;
    /** 秒；0 表示不限。 */
    final int timeout;
    final long startedAt = System.currentTimeMillis();
    /** 交给 PlayerEngine 的指令；attack 没有。 */
    final String command;

    /** collect / give：要数的物品，和开始时背包里有几个。 */
    String item;
    int want;
    int before;
    /** give：扔给玩家的个数（伙伴扔出这种物品时数，不看背包少了几个：fetch 时要先弄来再扔，背包里是先多后少）。 */
    int thrown;
    /** follow / give：玩家名。 */
    String player;
    /** attack：对应的 PlayerEngine 任务。 */
    AttackTask attack;

    boolean done;

    TaskRun(long id, int companion, String op, JsonObject args, int timeout, String command) {
        this.id = id;
        this.companion = companion;
        this.op = op;
        this.args = args;
        this.timeout = timeout;
        this.command = command;
    }

    long deadline() {
        return timeout <= 0 ? 0 : startedAt + timeout * 1000L;
    }

    JsonObject json() {
        JsonObject o = new JsonObject();
        o.addProperty("id", id);
        o.addProperty("companion", companion);
        o.addProperty("op", op);
        o.add("args", args.deepCopy());
        o.addProperty("timeout", timeout);
        o.addProperty("startedAt", startedAt);
        if (command != null) {
            o.addProperty("command", command);
        }
        return o;
    }
}
