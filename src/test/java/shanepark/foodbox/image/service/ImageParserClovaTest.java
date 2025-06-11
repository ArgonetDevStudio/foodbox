package shanepark.foodbox.image.service;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.core.io.ClassPathResource;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.ocr.ImageMarginCalculator;
import shanepark.foodbox.image.ocr.ImageMarginCalculatorDaejeon;
import shanepark.foodbox.image.ocr.clova.ImageParserClova;
import shanepark.foodbox.image.ocr.clova.NaverClovaApi;

import java.io.IOException;
import java.time.Clock;
import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneId;
import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class ImageParserClovaTest {
    @Mock
    NaverClovaApi naverClovaApi;

    @Mock
    Clock clock;

    void mockClock(LocalDate date) {
        ZoneId zoneId = ZoneId.systemDefault();
        Instant fixedInstant = date.atStartOfDay(zoneId).toInstant();
        when(clock.instant()).thenReturn(fixedInstant);
        when(clock.getZone()).thenReturn(zoneId);
    }

    @Test
    void parse() throws IOException {
        // Given
        ImageMarginCalculator imageMarginCalculatorOfficial = new ImageMarginCalculatorDaejeon();
        mockClock(LocalDate.of(2025, 6, 11));
        ImageParserClova imageParserClova = new ImageParserClova(imageMarginCalculatorOfficial, naverClovaApi, clock);

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        ClassPathResource nov11 = new ClassPathResource("menu/menu-daejeon-1000034805.jpg");

        // When
        when(naverClovaApi.clovaRequest(anyString())).thenReturn(clovaResponse);
        List<ParsedMenu> parse = imageParserClova.parse(nov11.getFile().toPath());

        // Then
        assertThat(parse).hasSize(10);

        ParsedMenu first = parse.getFirst();
        assertThat(first.getDate()).isEqualTo(LocalDate.of(2025, 6, 9));

        assertThat(String.join(", ", parse.get(0).getMenus())).isEqualTo("흑미밥, 매콤 안동찜닭(납작당면), 치즈 옥수수전, 견과류병아리콩조림, 도라지 조미어채무침, 청경채 겉절이, 깍두기, 시래기된장국, [케이준 치킨텐더 샐러드]");
        assertThat(String.join(", ", parse.get(1).getMenus())).isEqualTo("흰쌀밥, 떡갈비, 난자완스&새우튀김, 야채 계란전, 마카로니 샐러드, 이호박야채볶음, 배추김치, 시원칼칼 콩나물국, [크로와상 햄&치즈 샐러드]");
        assertThat(String.join(", ", parse.get(2).getMenus())).isEqualTo("기장밥, 비벼먹는 스팸돈부리, (Feat. 쪽파), 삼색 호박채전, 팝스감자튀김, 꽈리 고추 멸치볶음, 백김치, 얼큰 순두부국, [쉬림프 푸실리 샐러드]");
        assertThat(String.join(", ", parse.get(3).getMenus())).isEqualTo("흰쌀밥(+계란후라이), 소고기 하이라이스, 생선까스(+타르소스), 단짠 미트볼 조림, 참치 볶음 김치, 오이 미역 무침, 초록나물 겉절이, 배추새우젓국, [에그마요 맥시칸 샐러드]");
        assertThat(String.join(", ", parse.get(4).getMenus())).isEqualTo("기장밥, 깻잎 쌈장 제육볶음, 베이컨 송송 김치전, 고구마 치즈볼 1P, 부추 김자반볶음, 모듬 견과류 조림, 배추김치, 맑은 수제비국, [닭가슴살 감자야채 샐러드]");
        assertThat(String.join(", ", parse.get(5).getMenus())).isEqualTo("보리 쌀밥, 체다 치즈 올라간, 베이컨 부대볶음, 미나리 카레 부침개, 비엔나 머스타드 볶음, 조미어채 견과류 조림, 알마늘 장아찌, 백김치, 김 계란국, [담백 오리훈제 샐러드]");
        assertThat(String.join(", ", parse.get(6).getMenus())).isEqualTo("후리 가케 라이스, 불고기 마요소스, 멘치까스(+양상추), 칠리 시즈닝 감튀, 참깨소스 브로콜리, 볼어묵 콩고기 조림, 대파 겉절이, 배추 김치, 유부 우동국, [베이컨 토마토스파게티]");
        assertThat(String.join(", ", parse.get(7).getMenus())).isEqualTo("기장밥, 양푼식 매운 목살갈비찜, (+콩나물 추가), 괭이버섯 야채전, 맛살채 마늘쫑 볶음, 달달 고구마 맛탕, 할라피뇨 치킨무, 배추 김치, 사골 미역국, [참치마요 또띠아 샐러드]");
        assertThat(String.join(", ", parse.get(8).getMenus())).isEqualTo("잡곡밥, 데리야끼소스, 단호박 오리훈제볶음, 쫄깃 어묵채전, 단판 메추리알 장조림, 목이버섯 굴소스 볶음, 오늘의 반찬 1종, 양념 단무지, 북어뭇국, [뉴욕 칠리 핫도그 샐러드]");
        assertThat(String.join(", ", parse.get(9).getMenus())).isEqualTo("새우살 야채볶음밥, 중화풍 유니짜장, 철판 사각 군만두 2P, 오징어바 튀김, 튀긴마늘 멸치 볶음, 오징어 젓갈 무침, 깍두기, 김치 두부국, [케이준 치킨텐더 샐러드]");
    }

}
