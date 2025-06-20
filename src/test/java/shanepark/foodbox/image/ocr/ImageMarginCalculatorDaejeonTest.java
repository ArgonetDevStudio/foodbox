package shanepark.foodbox.image.ocr;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import org.junit.jupiter.api.Test;
import org.springframework.core.io.ClassPathResource;
import shanepark.foodbox.image.domain.DayRegion;
import shanepark.foodbox.image.domain.ParseRegion;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.IOException;
import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;

class ImageMarginCalculatorDaejeonTest {
    ImageMarginCalculator calculator = new ImageMarginCalculatorDaejeon();

    @Test
    void calcParseRegions() throws IOException {
        ClassPathResource jun9 = new ClassPathResource("menu/menu-daejeon-1000034805.jpg");
        BufferedImage image = ImageIO.read(jun9.getInputStream());

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        JsonObject jsonObject = new Gson().fromJson(clovaResponse, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonArray fields = images.get(0).getAsJsonObject().getAsJsonArray("fields");

        // When
        List<DayRegion> dayRegions = calculator.calcParseRegions(image, fields);

        DayRegion first = dayRegions.getFirst();
        assertThat(first.date()).isEqualTo(new ParseRegion(48, 609, 154, 55));
        assertThat(first.menu()).isEqualTo(new ParseRegion(48, 664, 154, 176));

        DayRegion last = dayRegions.getLast();
        assertThat(last.date()).isEqualTo(new ParseRegion(672, 957, 154, 55));
        assertThat(last.menu()).isEqualTo(new ParseRegion(672, 1012, 154, 297));

        // Then
        assertThat(dayRegions).hasSize(10);
    }

    @Test
    void calcParseRegions2() throws IOException {
        ClassPathResource jun23 = new ClassPathResource("menu/1000035171.jpg");
        BufferedImage image = ImageIO.read(jun23.getInputStream());

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response-1000035171.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        JsonObject jsonObject = new Gson().fromJson(clovaResponse, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonArray fields = images.get(0).getAsJsonObject().getAsJsonArray("fields");

        // When
        List<DayRegion> dayRegions = calculator.calcParseRegions(image, fields);

        DayRegion first = dayRegions.getFirst();
        assertThat(first.date()).isEqualTo(new ParseRegion(48, 609, 155, 60));
        assertThat(first.menu()).isEqualTo(new ParseRegion(48, 669, 155, 208));

        DayRegion last = dayRegions.getLast();
        assertThat(last.date()).isEqualTo(new ParseRegion(676, 958, 155, 61));
        assertThat(last.menu()).isEqualTo(new ParseRegion(676, 1019, 155, 210));

        // Then
        assertThat(dayRegions).hasSize(10);
    }

}
