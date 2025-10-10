using System;

public class MixMax {
    public static void Main(string[] args) {
        int[] numbers = { 5, 2, 10, 1, 7 };
        int mix = 0;
        int max = 0;

        if (numbers.Length > 0) {
            mix = numbers[0];
            max = numbers[0];

            foreach (int number in numbers) {
                if (number < mix) {
                    mix = number;
                }
                if (number > max) {
                    max = number;
                }
            }
        }

        Console.WriteLine("Mix: " + mix);
        Console.WriteLine("Max: " + max);
    }
}