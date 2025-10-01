package shanepark.foodbox.utils;

public class ImageUtil {

    public static boolean isBlack(int color) {
        int r = (color >> 16) & 0xFF;
        int g = (color >> 8) & 0xFF;
        int b = color & 0xFF;
        return r + g + b < 32;
    }

}
