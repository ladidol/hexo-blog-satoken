package org.cuit.epoch.controller;

import cn.dev33.satoken.stp.StpUtil;
import cn.dev33.satoken.util.SaResult;
import cn.hutool.core.bean.BeanUtil;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.toolkit.StringUtils;
import eu.bitwalker.useragentutils.UserAgent;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.entity.UserAuth;
import org.cuit.epoch.entity.UserInfo;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.mapper.RoleMapper;
import org.cuit.epoch.mapper.UserAuthMapper;
import org.cuit.epoch.mapper.UserInfoMapper;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.util.IpUtils;
import org.cuit.epoch.util.PasswordUtils;
import org.cuit.epoch.util.Result;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.List;
import java.util.Objects;
import java.util.Set;

import static org.cuit.epoch.enums.RedisPrefixConst.*;
import static org.cuit.epoch.enums.ZoneEnum.SHANGHAI;

/**
 * @author: ladidol
 * @date: 2022/11/16 15:35
 * @description:
 */
@RestController
@Slf4j
public class LoginController {

    @Autowired
    private UserAuthMapper userAuthMapper;

    @Autowired
    private UserInfoMapper userInfoMapper;
    @Autowired
    private RoleMapper roleDao;
    @Autowired
    private RedisService redisService;

    @Resource
    private HttpServletRequest request;


    @RequestMapping("login")
    public Result<UserDetailDTO> login(String username, String password) {
//    public SaResult login(String username, String password, HttpServletRequest request) {//不知道这里的request能不能用

        if (StringUtils.isBlank(username)) {
            throw new AppException("用户名不能为空！");
        }
        // 查询账号是否存在
        UserAuth userAuth = userAuthMapper.selectOne(new LambdaQueryWrapper<UserAuth>()
                .select(UserAuth::getId, UserAuth::getUserInfoId, UserAuth::getUsername, UserAuth::getPassword, UserAuth::getLoginType)
                .eq(UserAuth::getUsername, username));
        if (Objects.isNull(userAuth)) {
            throw new AppException("用户名不存在!");
        }
        // 封装登录信息
        UserDetailDTO userDetailDTO = convertUserDetail(userAuth, request);
        log.info("userDetailDTO = " + userDetailDTO);

        if (!userDetailDTO.getPassword().equals(PasswordUtils.encrypt(password))) {
            throw new AppException("密码错误！");
        }
        StpUtil.login(userDetailDTO.getId());
        return Result.ok(userDetailDTO);
    }

    /**
     * 封装用户登录信息
     *
     * @param user    用户账号
     * @param request 请求
     * @return 用户登录信息
     */
    public UserDetailDTO convertUserDetail(UserAuth user, HttpServletRequest request) {
        // 查询账号信息
        UserInfo userInfo = userInfoMapper.selectById(user.getUserInfoId());
        // 查询账号角色
        List<String> roleList = roleDao.listRolesByUserInfoId(userInfo.getId());
        // 查询账号点赞信息
        Set<Object> articleLikeSet = redisService.sMembers(ARTICLE_USER_LIKE + userInfo.getId());
        Set<Object> commentLikeSet = redisService.sMembers(COMMENT_USER_LIKE + userInfo.getId());
        Set<Object> talkLikeSet = redisService.sMembers(TALK_USER_LIKE + userInfo.getId());
        // 获取设备信息
        String ipAddress = IpUtils.getIpAddress(request);
        String ipSource = IpUtils.getIpSource(ipAddress);
        UserAgent userAgent = IpUtils.getUserAgent(request);
        // 封装权限集合
        return UserDetailDTO.builder()
                .id(user.getId())
                .loginType(user.getLoginType())
                .userInfoId(userInfo.getId())
                .username(user.getUsername())
                .password(user.getPassword())
                .email(userInfo.getEmail())
                .roleList(roleList)
                .nickname(userInfo.getNickname())
                .avatar(userInfo.getAvatar())
                .intro(userInfo.getIntro())
                .webSite(userInfo.getWebSite())
                .articleLikeSet(articleLikeSet)
                .commentLikeSet(commentLikeSet)
                .talkLikeSet(talkLikeSet)
                .ipAddress(ipAddress)
                .ipSource(ipSource)
                .isDisable(userInfo.getIsDisable())
                .browser(userAgent.getBrowser().getName())
                .os(userAgent.getOperatingSystem().getName())
                .lastLoginTime(LocalDateTime.now(ZoneId.of(SHANGHAI.getZone())))
                .build();
    }

}