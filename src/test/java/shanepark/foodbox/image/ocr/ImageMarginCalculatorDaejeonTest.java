package shanepark.foodbox.image.ocr;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import org.assertj.core.data.Offset;
import org.junit.jupiter.api.DisplayName;
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
    Offset<Integer> offset = Offset.offset(2);

    @Test
    void calcParseRegions() throws IOException {
        ClassPathResource jun9 = new ClassPathResource("menu/20250609.jpg");
        BufferedImage image = ImageIO.read(jun9.getInputStream());

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response-20250609.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        JsonObject jsonObject = new Gson().fromJson(clovaResponse, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonArray fields = images.get(0).getAsJsonObject().getAsJsonArray("fields");

        // When
        List<DayRegion> dayRegions = calculator.calcParseRegions(image, fields);

        DayRegion first = dayRegions.getFirst();
        assertThat(first.date().x()).isCloseTo(49, offset);
        assertThat(first.date().y()).isCloseTo(609, offset);
        assertThat(first.date().width()).isCloseTo(155, offset);
        assertThat(first.date().height()).isCloseTo(55, offset);

        assertThat(first.menu().x()).isCloseTo(49, offset);
        assertThat(first.menu().y()).isCloseTo(664, offset);
        assertThat(first.menu().width()).isCloseTo(155, offset);
        assertThat(first.menu().height()).isCloseTo(176, offset);

        DayRegion last = dayRegions.getLast();
        assertThat(last.date().x()).isCloseTo(673, offset);
        assertThat(last.date().y()).isCloseTo(957, offset);
        assertThat(last.date().width()).isCloseTo(155, offset);
        assertThat(last.date().height()).isCloseTo(55, offset);

        assertThat(last.menu().x()).isCloseTo(673, offset);
        assertThat(last.menu().y()).isCloseTo(1012, offset);
        assertThat(last.menu().width()).isCloseTo(155, offset);
        assertThat(last.menu().height()).isCloseTo(297, offset);

        // Then
        assertThat(dayRegions).hasSize(10);
    }

    @Test
    void calcParseRegions2() throws IOException {
        ClassPathResource jun23 = new ClassPathResource("menu/20250623.jpg");
        BufferedImage image = ImageIO.read(jun23.getInputStream());

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response-20250623.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        JsonObject jsonObject = new Gson().fromJson(clovaResponse, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonArray fields = images.get(0).getAsJsonObject().getAsJsonArray("fields");

        // When
        List<DayRegion> dayRegions = calculator.calcParseRegions(image, fields);

        DayRegion first = dayRegions.getFirst();
        assertThat(first.date().x()).isCloseTo(48, offset);
        assertThat(first.date().y()).isCloseTo(609, offset);
        assertThat(first.date().width()).isCloseTo(156, offset);
        assertThat(first.date().height()).isCloseTo(60, offset);

        assertThat(first.menu().x()).isCloseTo(48, offset);
        assertThat(first.menu().y()).isCloseTo(669, offset);
        assertThat(first.menu().width()).isCloseTo(156, offset);
        assertThat(first.menu().height()).isCloseTo(207, offset);

        DayRegion last = dayRegions.getLast();
        assertThat(last.menu().x()).isCloseTo(676, offset);
        assertThat(last.menu().y()).isCloseTo(1020, offset);
        assertThat(last.menu().width()).isCloseTo(156, offset);
        assertThat(last.menu().height()).isCloseTo(209, offset);

        assertThat(last.menu().x()).isCloseTo(676, offset);
        assertThat(last.menu().y()).isCloseTo(1019, offset);
        assertThat(last.menu().width()).isCloseTo(156, offset);
        assertThat(last.menu().height()).isCloseTo(209, offset);

        // Then
        assertThat(dayRegions).hasSize(10);
    }

    @Test
    @DisplayName("parse high quality image")
    void calcParseRegions3() throws IOException {
        ClassPathResource jun23 = new ClassPathResource("menu/20250707.jpg");
        BufferedImage image = ImageIO.read(jun23.getInputStream());

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response-20250707.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        JsonObject jsonObject = new Gson().fromJson(clovaResponse, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonArray fields = images.get(0).getAsJsonObject().getAsJsonArray("fields");

        // When
        List<DayRegion> dayRegions = calculator.calcParseRegions(image, fields);

        DayRegion first = dayRegions.getFirst();
        assertThat(first.date()).isEqualTo(new ParseRegion(120, 1521, 386, 154));
        assertThat(first.date().x()).isEqualTo(120);
        assertThat(first.date().y()).isEqualTo(1521);
        assertThat(first.date().width()).isEqualTo(386);
        assertThat(first.date().height()).isEqualTo(154);

        assertThat(first.menu()).isEqualTo(new ParseRegion(120, 1675, 386, 532));
        assertThat(first.menu().x()).isEqualTo(120);
        assertThat(first.menu().y()).isEqualTo(1675);
        assertThat(first.menu().width()).isEqualTo(386);
        assertThat(first.menu().height()).isEqualTo(532);

        DayRegion last = dayRegions.getLast();
        assertThat(last.date().x()).isCloseTo(1688, offset);
        assertThat(last.date().y()).isCloseTo(2418, offset);
        assertThat(last.date().width()).isEqualTo(386);
        assertThat(last.date().height()).isEqualTo(154);

        assertThat(last.menu().x()).isCloseTo(1688, offset);
        assertThat(last.menu().y()).isCloseTo(2572, offset);
        assertThat(last.menu().width()).isEqualTo(386);
        assertThat(last.menu().height()).isEqualTo(533);

        // Then
        assertThat(dayRegions).hasSize(10);
    }

}
