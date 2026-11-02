package minedeck.voicebridge;

/**
 * 自测音：C 大调琶音（C5 E5 G5 C6 G5 E5）循环，每个音 0.3 秒，音量约 -12 dBFS。
 * 每个音都有起音和收尾的渐变，音与音之间不会有爆音。
 */
final class SelfTestTone {

    private static final double[] NOTES = {523.25, 659.25, 783.99, 1046.50, 783.99, 659.25};
    private static final int NOTE_SAMPLES = Protocol.SAMPLE_RATE * 300 / 1000;
    private static final int ATTACK = Protocol.SAMPLE_RATE * 5 / 1000;
    private static final int RELEASE = Protocol.SAMPLE_RATE * 60 / 1000;
    private static final double AMPLITUDE = 0.25 * 32767;

    private long position;

    short[] next() {
        short[] frame = new short[Protocol.FRAME_SAMPLES];
        for (int i = 0; i < frame.length; i++) {
            long n = position + i;
            int note = (int) ((n / NOTE_SAMPLES) % NOTES.length);
            int t = (int) (n % NOTE_SAMPLES);
            double envelope = 1.0;
            if (t < ATTACK) {
                envelope = t / (double) ATTACK;
            } else if (t > NOTE_SAMPLES - RELEASE) {
                envelope = (NOTE_SAMPLES - t) / (double) RELEASE;
            }
            double v = Math.sin(2 * Math.PI * NOTES[note] * t / Protocol.SAMPLE_RATE) * envelope * AMPLITUDE;
            frame[i] = (short) Math.round(v);
        }
        position += frame.length;
        return frame;
    }

    void reset() {
        position = 0;
    }
}
