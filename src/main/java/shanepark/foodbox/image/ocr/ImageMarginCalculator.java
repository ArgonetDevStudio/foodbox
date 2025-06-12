package shanepark.foodbox.image.ocr;

import com.google.gson.JsonArray;
import shanepark.foodbox.image.domain.DayRegion;

import java.awt.image.BufferedImage;
import java.util.List;

public interface ImageMarginCalculator {
    List<DayRegion> calcParseRegions(BufferedImage image, JsonArray fields);
}
