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
        for (int i = 0; i < 5; i++) {
            int y = gridParser.getRow1Start();
            int dayHeight = gridParser.getRow1DateHeight();
            int menuHeight = gridParser.getRow1MenuHeight();
            ParseRegion dateRegion = new ParseRegion(x, y, WIDTH, dayHeight);
            ParseRegion menuRegion = new ParseRegion(x, y + dayHeight, WIDTH, menuHeight);
            list.add(new DayRegion(dateRegion, menuRegion));
            x += WIDTH + GridParser.GAP;
        }
        x = gridParser.getStartX();
        for (int i = 0; i < 5; i++) {
            int y = gridParser.getRow2Start();
            int dayHeight = gridParser.getRow2DateHeight();
            int menuHeight = Math.min(image.getHeight(), GridParser.WARNING_TEXT) - (y + dayHeight);
            ParseRegion dateRegion = new ParseRegion(x, y, WIDTH, dayHeight);
            ParseRegion menuRegion = new ParseRegion(x, y + dayHeight, WIDTH, menuHeight);
            list.add(new DayRegion(dateRegion, menuRegion));
            x += WIDTH + GridParser.GAP;
        }
        return list;
    }

}
