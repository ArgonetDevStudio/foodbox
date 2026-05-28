package shanepark.foodbox.image.ocr;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import org.junit.jupiter.api.Test;
import shanepark.foodbox.image.domain.DayRegion;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;

class ImageMarginCalculatorEisoTest {

    private final ImageMarginCalculatorEiso calculator = new ImageMarginCalculatorEiso();
    private final Gson gson = new Gson();

    @Test
    void testCalcParseRegions() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202510.jpg");
        Path jsonPath = Paths.get("src/test/resources/eiso_202510.json");

        BufferedImage image = ImageIO.read(imagePath.toFile());
        String jsonContent = Files.readString(jsonPath);

        JsonObject jsonObject = gson.fromJson(jsonContent, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonObject imageObj = images.get(0).getAsJsonObject();
        JsonArray fields = imageObj.getAsJsonArray("fields");

        // When
        List<DayRegion> dayRegions = calculator.calcParseRegions(image, fields);

        // Then
        assertThat(dayRegions).isNotEmpty();
        System.out.println("Total day regions: " + dayRegions.size());

        for (int i = 0; i < dayRegions.size(); i++) {
            DayRegion region = dayRegions.get(i);
            System.out.printf("Region %d: Date[x=%d, y=%d, w=%d, h=%d] Menu[x=%d, y=%d, w=%d, h=%d]%n",
                    i,
                    region.date().x(), region.date().y(), region.date().width(), region.date().height(),
                    region.menu().x(), region.menu().y(), region.menu().width(), region.menu().height());
        }
    }

    @Test
    void calcParseRegionsWithNumericDateOnlyLayout() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202605.png");
        Path jsonPath = Paths.get("src/test/resources/eiso_202605.json");

        BufferedImage image = ImageIO.read(imagePath.toFile());
        String jsonContent = Files.readString(jsonPath);

        JsonObject jsonObject = gson.fromJson(jsonContent, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonObject imageObj = images.get(0).getAsJsonObject();
        JsonArray fields = imageObj.getAsJsonArray("fields");

        // When
        List<DayRegion> dayRegions = calculator.calcParseRegions(image, fields);

        // Then
        assertThat(dayRegions).hasSize(25);
    }
}
