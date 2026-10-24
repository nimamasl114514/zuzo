// BenchMulti.java — Java (HotSpot JIT) 基准
public class BenchMulti {
    static final long N = 100000;
    static final long ROUNDS = 100000;

    public static void main(String[] args) {
        // 预热 JIT
        long warmup = 0;
        for (int w = 0; w < 10000; w++) {
            long s = 0;
            for (long i = 0; i < N; i++) s += i;
            warmup += s;
        }
        System.out.println("Warmup total: " + warmup);

        long start = System.nanoTime();

        long total = 0;
        for (long r = 0; r < ROUNDS; r++) {
            long sum = 0;
            for (long i = 0; i < N; i++) {
                sum += i;
            }
            total += sum;
        }

        long elapsedNs = System.nanoTime() - start;
        double perIterNs = (double) elapsedNs / (ROUNDS * N);
        double perRoundUs = (double) elapsedNs / ROUNDS / 1000.0;
        double elapsedS = elapsedNs / 1e9;

        System.out.printf("Java (JIT):      %.2f ns/iter  |  %.2f us/round  |  total=%d  |  %.3fs%n",
                perIterNs, perRoundUs, total, elapsedS);
    }
}
