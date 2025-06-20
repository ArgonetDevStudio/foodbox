package shanepark.foodbox.image.ocr;

import com.google.gson.JsonArray;
import org.springframework.stereotype.Component;
import shanepark.foodbox.image.domain.DayRegion;
import shanepark.foodbox.image.domain.GridParser;
import shanepark.foodbox.image.domain.ParseRegion;

import java.awt.image.BufferedImage;
import java.util.ArrayList;
import java.util.List;

@Component
public class ImageMarginCalculatorDaejeon implements ImageMarginCalculator {

    @Override
    public List<DayRegion> calcParseRegions(BufferedImage image, JsonArray fields) {
        GridParser gridParser = new GridParser(fields);
        int WIDTH = gridParser.getWidth();

        List<DayRegion> list = new ArrayList<>();
        int x = gridParser.getStartX();
        int row1MenuEndY = 0;
        for (int i = 0; i < 5; i++) {
            int y = gridParser.getRow1Start();
            int dayHeight = gridParser.getRow1DateHeight();
            if (row1MenuEndY == 0) {
                row1MenuEndY = findRowMenuEndY(image, gridParser.getMenu1DateEnd(), gridParser);
            }
            int menuHeight = row1MenuEndY - (y + dayHeight);
            ParseRegion dateRegion = new ParseRegion(x, y, WIDTH, dayHeight);
            ParseRegion menuRegion = new ParseRegion(x, y + dayHeight, WIDTH, menuHeight);
            list.add(new DayRegion(dateRegion, menuRegion));
            x += WIDTH + GridParser.GAP;
        }
        x = gridParser.getStartX();
        int row2MenuEndY = 0;
        for (int i = 0; i < 5; i++) {
            int y = gridParser.getRow2Start();
            int dayHeight = gridParser.getRow2DateHeight();
            if (row2MenuEndY == 0) {
                row2MenuEndY = findRowMenuEndY(image, gridParser.getMenu2DateEnd(), gridParser);
            }
            int menuHeight = row2MenuEndY - (y + dayHeight);
            ParseRegion dateRegion = new ParseRegion(x, y, WIDTH, dayHeight);
            ParseRegion menuRegion = new ParseRegion(x, y + dayHeight, WIDTH, menuHeight);
            list.add(new DayRegion(dateRegion, menuRegion));
            x += WIDTH + GridParser.GAP;
        }
        return list;
    }

    private int findRowMenuEndY(BufferedImage image, int menuDateEnd, GridParser gridParser) {
        int height = image.getHeight();
        int blackLast = -1;
        int x = gridParser.getStartX() + GridParser.GAP * 2;
        for (int y = menuDateEnd; y < height; y++) {
            boolean isBlack = isBlack(image.getRGB(x, y));
            if (!isBlack) {
                continue;
            }
            if (blackLast == -1) {
                blackLast = y;
                continue;
            }
            if (y - blackLast > 10) {
                return y;
            }
            blackLast = y;
        }
        throw new IllegalStateException("Could not find row1 menu end Y");
    }

    boolean isBlack(int color) {
        int r = (color >> 16) & 0xFF;
        int g = (color >> 8) & 0xFF;
        int b = color & 0xFF;
        return r < 15 && g < 15 && b < 15;
    }

}
