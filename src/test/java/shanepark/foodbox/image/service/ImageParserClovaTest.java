package shanepark.foodbox.image.service;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.core.io.ClassPathResource;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.ocr.ImageMarginCalculator;
import shanepark.foodbox.image.ocr.clova.ImageParserClova;
import shanepark.foodbox.image.ocr.clova.NaverClovaApi;

import java.io.IOException;
import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class ImageParserClovaTest {
    @Mock
    NaverClovaApi naverClovaApi;

    @Test
    void parse() throws IOException {
        // Given
        ImageMarginCalculator imageMarginCalculator = new ImageMarginCalculator();
        ImageParserClova imageParserClova = new ImageParserClova(imageMarginCalculator, naverClovaApi);

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        ClassPathResource nov11 = new ClassPathResource("menu/menu-11112024.png");

        // When
        when(naverClovaApi.clovaRequest(anyString())).thenReturn(clovaResponse);
        List<ParsedMenu> parse = imageParserClova.parse(nov11.getFile().toPath());

        // Then
        assertThat(parse).hasSize(10);

        ParsedMenu first = parse.getFirst();
        assertThat(first.getDate()).isEqualTo("11/11(월)");

        List<String> firstMenu = first.getMenus();
        assertThat(firstMenu).hasSize(9);
        assertThat(firstMenu.get(0)).isEqualTo("흰쌀밥");
        assertThat(firstMenu.get(1)).isEqualTo("데미글라스 소스");
        assertThat(firstMenu.get(2)).isEqualTo("스크램블 함박");
        assertThat(firstMenu.get(3)).isEqualTo("설탕 프렌치 토스트");
        assertThat(firstMenu.get(4)).isEqualTo("어묵 맛살 볶음");
        assertThat(firstMenu.get(5)).isEqualTo("살사 푸실리 샐러드");
        assertThat(firstMenu.get(6)).isEqualTo("시금치 나물 무침");
        assertThat(firstMenu.get(7)).isEqualTo("배추 김치");
        assertThat(firstMenu.get(8)).isEqualTo("버섯 고추장국");
    }


    @Test
    @DisplayName("image parse test 20250526")
    void parse6() throws IOException {
        // Given
        ImageMarginCalculator imageMarginCalculator = new ImageMarginCalculator();
        ImageParserClova imageParserClova = new ImageParserClova(imageMarginCalculator, naverClovaApi);

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response20250526.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        ClassPathResource image = new ClassPathResource("menu/menu-20250526.png");

        // When
        when(naverClovaApi.clovaRequest(anyString())).thenReturn(clovaResponse);
        List<ParsedMenu> parse = imageParserClova.parse(image.getFile().toPath());

        assertThat(parse).hasSize(10);
    }

}
