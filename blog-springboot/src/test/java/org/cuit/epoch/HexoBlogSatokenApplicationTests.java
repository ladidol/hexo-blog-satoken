package org.cuit.epoch;

import org.cuit.epoch.util.PasswordUtils;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;

@SpringBootTest
class HexoBlogSatokenApplicationTests {

    @Test
    void contextLoads() {
    }

    @Test
    void getEncodePassword(){
        System.out.println(PasswordUtils.encrypt("nihao123"));
    }





}
